//go:build integration

package integration

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const g5CoexistenceEvidenceEnv = "SESAMEFS_REQUIRE_G5_COEXISTENCE_EVIDENCE"

// A peer daemon can consume the old root before the fresh worker under test.
// Preserve the original n>=1 and all exact-P/byte/ref assertions by running
// only this manual continuation test in the existing isolated keyspace.
func g5RunCoexistenceIsolated(t *testing.T, endpoint string) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestG5CassandraMinIOOldLifeWithoutDayScheduling$", "-test.v", "-test.count=1", "-test.timeout=90s")
	for _, entry := range os.Environ() {
		name := strings.SplitN(entry, "=", 2)[0]
		if strings.HasPrefix(name, "SESAMEFS_REQUIRE_") || strings.HasSuffix(name, "_CHILD") || name == "SESAMEFS_URL" || name == "SESAMEFS_URL_2" || name == "SESAMEFS_URL_3" || name == "CASSANDRA_KEYSPACE" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, g5CoexistenceEvidenceEnv+"=1", "SESAMEFS_G5_COEXISTENCE_CHILD=1", "CASSANDRA_KEYSPACE=sesamefs_e19", "SESAMEFS_URL="+endpoint, "SESAMEFS_URL_2="+endpoint, "SESAMEFS_URL_3="+endpoint)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("isolated G5 old-life continuation failed: %v", err)
	}
	// Child TestMain rejects unavailable/skipped/omitted evidence after teardown.
	g5CoexistenceObserved = true
}
