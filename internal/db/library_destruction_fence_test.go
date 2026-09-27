package db

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

func TestPCD1B5MigrationAddsOnlyCanonicalFenceColumns(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	migration, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "migrations", "028_continuity_destruction_fence.cql"))
	if err != nil {
		t.Fatalf("read migration 028: %v", err)
	}
	text := strings.ToLower(string(migration))
	for _, column := range []string{
		"continuity_destruction_epoch timeuuid",
		"continuity_destruction_pending map<uuid, timeuuid>",
		"continuity_destruction_superseded timeuuid",
	} {
		if !strings.Contains(text, column) {
			t.Errorf("migration 028 is missing canonical fence column %q", column)
		}
	}
	if strings.Contains(text, "create table") || strings.Contains(text, "root_digest") || strings.Contains(text, "continuity_certified_head_commit_id") {
		t.Fatal("migration 028 must add only E/P/S to libraries and leave witness shape unchanged")
	}
}

func destructionFenceFunctionSource(t *testing.T, name string) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	sourceBytes, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "library_destruction_fence.go"))
	if err != nil {
		t.Fatalf("read destruction fence implementation: %v", err)
	}
	source := string(sourceBytes)
	signature := "func " + name + "("
	start := strings.Index(source, signature)
	if start < 0 {
		t.Fatalf("function %s not found", name)
	}
	body := source[start:]
	if next := strings.Index(body[len(signature):], "\nfunc "); next >= 0 {
		body = body[:len(signature)+next]
	}
	return body
}

func TestPCD1B5DestructionFenceCQLContracts(t *testing.T) {
	capture := destructionFenceFunctionSource(t, "CaptureDestructionFence")
	if !strings.Contains(capture, "Consistency(gocql.Serial)") || !strings.Contains(capture, "continuity_destruction_epoch") ||
		!strings.Contains(capture, "continuity_destruction_pending") || !strings.Contains(capture, "continuity_destruction_superseded") {
		t.Fatal("CW-M8/CW-M4: capture must read E/P/S in the explicit global SERIAL domain")
	}
	if strings.Contains(capture, "LocalSerial") || !strings.Contains(capture, "Status: DestructionFenceUnknown") {
		t.Fatal("capture must not use LOCAL_SERIAL or collapse unavailable state into idle")
	}

	begin := destructionFenceFunctionSource(t, "BeginDestructionIntent")
	for _, required := range []string{
		"continuity_destruction_epoch = ?",
		"continuity_destruction_pending[?] = ?",
		"continuity_destruction_superseded = ?",
		"continuity_certified_head_commit_id = null",
		"continuity_contract_version = null",
		"created_at = ?",
		"SerialConsistency(LibraryHeadSerialConsistency)",
		"generation.Timestamp() <= observed.Epoch.Timestamp()",
	} {
		if !strings.Contains(begin, required) {
			t.Errorf("CW-M2/M3/M8/M11/M21: intent is missing %q", required)
		}
	}
	if strings.Contains(begin, "IF EXISTS") || strings.Contains(begin, "LocalSerial") {
		t.Fatal("intent must predicate the observed canonical sentinel in global SERIAL, without a ghost-row fallback")
	}

	complete := destructionFenceFunctionSource(t, "CompleteDestructionIntent")
	if !strings.Contains(complete, "DELETE continuity_destruction_pending[?]") ||
		!strings.Contains(complete, "IF continuity_destruction_pending[?] = ?") ||
		!strings.Contains(complete, "SerialConsistency(LibraryHeadSerialConsistency)") {
		t.Fatal("CW-M6/M8/M16: completion must delete P[t] only under P[t]=g in global SERIAL")
	}
	if strings.Contains(complete, "IF continuity_destruction_epoch = ?") || strings.Contains(complete, "DELETE FROM libraries") {
		t.Fatal("completion ownership is per-token generation and must not use the global epoch")
	}
}

func TestPCD1B5TakeoverRaisesSupersededGenerationMonotonically(t *testing.T) {
	old := gocql.UUIDFromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	newerFloor := gocql.UUIDFromTime(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	olderReplacement := gocql.UUIDFromTime(time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC))
	if got := maxSupersededGeneration(nil, &old); got == nil || *got != old {
		t.Fatalf("first takeover floor = %v, want %s", got, old)
	}
	if got := maxSupersededGeneration(&newerFloor, &old); got == nil || *got != newerFloor {
		t.Fatalf("takeover lowered the existing superseded floor: got %v, want %s", got, newerFloor)
	}
	if got := maxSupersededGeneration(nil, &olderReplacement); got == nil || *got != olderReplacement {
		t.Fatalf("takeover must record its replaced owner, got %v want %s", got, olderReplacement)
	}
}

func TestPCD1B5SaturatedFenceAllowsTakeoverButRejectsNewToken(t *testing.T) {
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	epoch := gocql.UUIDFromTime(createdAt.Add(2 * time.Second))
	oldOwner := gocql.UUIDFromTime(createdAt.Add(time.Second))
	ownedToken := uuid.New()
	observed := DestructionFenceSnapshot{
		Status: DestructionFencePending, CreatedAt: createdAt, Epoch: &epoch,
		Pending: make(map[uuid.UUID]gocql.UUID, MaxOutstandingDestructionIntents),
	}
	for i := 0; i < MaxOutstandingDestructionIntents-1; i++ {
		observed.Pending[uuid.New()] = oldOwner
	}
	observed.Pending[ownedToken] = oldOwner
	if err := validateDestructionFenceSnapshot(observed); err != nil {
		t.Fatalf("full pending map rejected: %v", err)
	}
	if _, owns := observed.Pending[ownedToken]; !owns {
		t.Fatal("test precondition: retry token missing")
	}
	if len(observed.Pending) != MaxOutstandingDestructionIntents {
		t.Fatalf("pending count=%d, want cap %d", len(observed.Pending), MaxOutstandingDestructionIntents)
	}

	begin := destructionFenceFunctionSource(t, "BeginDestructionIntent")
	if !strings.Contains(begin, "len(observed.Pending) >= MaxOutstandingDestructionIntents") || !strings.Contains(begin, "ownsToken") {
		t.Fatal("new tokens must backpressure at the cap while existing tokens remain re-drivable")
	}
}

func TestPCD1B5CertifierRefusesUnreaffirmedSupersededGeneration(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	sourceBytes, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "library_continuity_certifier.go"))
	if err != nil {
		t.Fatalf("read certifier: %v", err)
	}
	source := string(sourceBytes)
	check := strings.Index(source, "if fence.Superseded != nil")
	refusal := -1
	witness := strings.Index(source, "CommitLibraryContinuityWitnessContext(ctx")
	if check >= 0 {
		refusal = strings.Index(source[check:], "LibraryBaselineReasonSupersededGenerationPending")
	}
	if check < 0 || refusal < 0 || witness < check {
		t.Fatal("a superseded generation must fail closed before witness CAS until global reaffirmation is implemented")
	}
}

func TestPCD1B5FenceSnapshotRejectsImpossibleStates(t *testing.T) {
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	epoch := gocql.UUIDFromTime(createdAt.Add(2 * time.Second))
	older := gocql.UUIDFromTime(createdAt.Add(time.Second))
	token := uuid.New()
	tests := []struct {
		name     string
		snapshot DestructionFenceSnapshot
	}{
		{
			name: "pending state without epoch",
			snapshot: DestructionFenceSnapshot{
				Status: DestructionFencePending, CreatedAt: createdAt,
				Pending: map[uuid.UUID]gocql.UUID{token: older},
			},
		},
		{
			name: "pending generation newer than epoch",
			snapshot: DestructionFenceSnapshot{
				Status: DestructionFencePending, CreatedAt: createdAt, Epoch: &older,
				Pending: map[uuid.UUID]gocql.UUID{token: epoch},
			},
		},
		{
			name: "superseded generation not below epoch",
			snapshot: DestructionFenceSnapshot{
				Status: DestructionFenceIdle, CreatedAt: createdAt, Epoch: &epoch, Superseded: &epoch,
			},
		},
		{
			name: "idle status with pending token",
			snapshot: DestructionFenceSnapshot{
				Status: DestructionFenceIdle, CreatedAt: createdAt, Epoch: &epoch,
				Pending: map[uuid.UUID]gocql.UUID{token: older},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateDestructionFenceSnapshot(test.snapshot); err == nil {
				t.Fatal("impossible E/P/S snapshot was accepted")
			}
		})
	}

	valid := DestructionFenceSnapshot{
		Status: DestructionFencePending, CreatedAt: createdAt, Epoch: &epoch, Superseded: &older,
		Pending: map[uuid.UUID]gocql.UUID{token: older},
	}
	if err := validateDestructionFenceSnapshot(valid); err != nil {
		t.Fatalf("valid pending snapshot rejected: %v", err)
	}
}

func TestPCD1B5CanonicalAbsenceProofRequiresBothGlobalReads(t *testing.T) {
	proof := destructionFenceFunctionSource(t, "ProveGlobalCanonicalAbsence")
	serialRead := strings.Index(proof, "Consistency(gocql.Serial)")
	serialPresentVeto := strings.Index(proof, "if serialErr == nil")
	serialUnknownVeto := strings.Index(proof, "!errors.Is(serialErr, gocql.ErrNotFound)")
	eachQuorumRead := strings.Index(proof, "Consistency(gocql.EachQuorum)")
	eachQuorumAbsent := strings.Index(proof, "!errors.Is(eachErr, gocql.ErrNotFound)")
	mint := strings.Index(proof, "minted: true")
	if serialRead < 0 || serialPresentVeto < 0 || serialUnknownVeto < 0 || eachQuorumRead < 0 || eachQuorumAbsent < 0 || mint < 0 ||
		!(serialRead < serialPresentVeto && serialPresentVeto < serialUnknownVeto && serialUnknownVeto < eachQuorumRead && eachQuorumRead < eachQuorumAbsent && eachQuorumAbsent < mint) {
		t.Fatalf("CW-M31/CW-M33: absence proof must require SERIAL-ABSENT then EACH_QUORUM-ABSENT: serial=%d present=%d unknown=%d each=%d absent=%d mint=%d", serialRead, serialPresentVeto, serialUnknownVeto, eachQuorumRead, eachQuorumAbsent, mint)
	}
	if strings.Contains(proof, "CanonicalLibraryExists") || strings.Contains(proof, "LocalQuorum") {
		t.Fatal("CW-M31: local absence must never mint GlobalCanonicalAbsenceProof")
	}
}
