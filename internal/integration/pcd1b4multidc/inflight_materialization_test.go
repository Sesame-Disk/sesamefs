//go:build integration

package pcd1b4multidc

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
	"github.com/google/uuid"
)

// pcd1b4WriteGate holds one client-to-coordinator CQL frame after the writer
// has passed admission and assigned its timestamp, but before Cassandra can
// settle it. The independent GC session can therefore commit a tombstone
// between admission and server-side application.
type pcd1b4WriteGate struct {
	armed    bool
	mu       sync.Mutex
	reached  chan struct{}
	release  chan struct{}
	entered  sync.Once
	released sync.Once
}

func newPCD1B4WriteGate() *pcd1b4WriteGate {
	return &pcd1b4WriteGate{reached: make(chan struct{}), release: make(chan struct{})}
}

func (g *pcd1b4WriteGate) arm() {
	g.mu.Lock()
	g.armed = true
	g.mu.Unlock()
}

func (g *pcd1b4WriteGate) unblock() {
	g.released.Do(func() { close(g.release) })
}

func (g *pcd1b4WriteGate) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	return &pcd1b4GatedConn{Conn: conn, gate: g}, nil
}

type pcd1b4GatedConn struct {
	net.Conn
	gate *pcd1b4WriteGate
}

func (c *pcd1b4GatedConn) Write(p []byte) (int, error) {
	c.gate.mu.Lock()
	armed := c.gate.armed
	if armed {
		c.gate.armed = false
	}
	c.gate.mu.Unlock()
	if armed {
		c.gate.entered.Do(func() { close(c.gate.reached) })
		<-c.gate.release
	}
	return c.Conn.Write(p)
}

func newPCD1B4GatedSession(t *testing.T, dc string, gate *pcd1b4WriteGate) *gocql.Session {
	t.Helper()
	addresses := strings.Split(strings.TrimSpace(os.Getenv(hostsEnv)), ",")
	var host string
	for _, address := range addresses {
		name, endpoint, ok := strings.Cut(strings.TrimSpace(address), "=")
		if ok && name == dc {
			host = endpoint
			break
		}
	}
	if host == "" {
		t.Fatalf("%s must contain %s=host:port for the isolated test", hostsEnv, dc)
	}

	cluster := gocql.NewCluster(host)
	cluster.Keyspace = "sesamefs"
	cluster.NumConns = 1
	cluster.ProtoVersion = 4
	cluster.DisableInitialHostLookup = true
	cluster.Consistency = gocql.EachQuorum
	cluster.SerialConsistency = gocql.Serial
	cluster.Timeout = 60 * time.Second
	cluster.ConnectTimeout = 15 * time.Second
	cluster.Dialer = gate
	if username, password := os.Getenv("CASSANDRA_USERNAME"), os.Getenv("CASSANDRA_PASSWORD"); username != "" && password != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{Username: username, Password: password}
	}
	session, err := cluster.CreateSession()
	if err != nil {
		t.Fatalf("open gated Cassandra session for %s: %v", dc, err)
	}
	t.Cleanup(session.Close)
	return session
}

// TestPCD1B4InFlightMaterialization3DC characterizes CW-M34 on the pinned
// Cassandra 5.0.9 fixture:
//
//	valid admission + timestamp w -> request paused before Cassandra
//	GC DELETE at g>w -> acknowledged by EACH_QUORUM
//	request resumes -> Cassandra acknowledges the old write
//
// The successful write remains invisible because its timestamp is below the
// tombstone. CW-M34 therefore requires a lifetime barrier or verified recovery;
// the health check at admission alone is not a sufficient contract.
func TestPCD1B4InFlightMaterialization3DC(t *testing.T) {
	if phase := strings.TrimSpace(os.Getenv(phaseEnv)); phase != "m34-inflight" {
		t.Skipf("%s=%q; run through the isolated 3-DC characterization script", phaseEnv, phase)
	}
	f := newFixture(t)
	fsID := testFSID("pc-d1b4-cw-m34-" + uuid.NewString())
	now := time.Now().UTC().UnixMicro()
	writerTS := now - int64(3*time.Second/time.Microsecond)
	deleteTS := now - int64(2*time.Second/time.Microsecond)
	if writerTS >= deleteTS || deleteTS >= now {
		t.Fatalf("CW-M34 timestamp precondition: writer=%d delete=%d now=%d", writerTS, deleteTS, now)
	}

	gate := newPCD1B4WriteGate()
	t.Cleanup(gate.unblock)
	defer gate.unblock()
	writer := newPCD1B4GatedSession(t, "dc-na", gate)
	deleter := connect(t, "dc-eu")

	gate.arm()
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- writer.Query(`
			INSERT INTO fs_objects (library_id, fs_id, obj_type, size_bytes)
			VALUES (?, ?, 'file', 0) USING TIMESTAMP ?
		`, f.libraryID, fsID, writerTS).Consistency(gocql.EachQuorum).Exec()
	}()

	select {
	case <-gate.reached:
	case err := <-writeDone:
		t.Fatalf("CW-M34 writer completed before the transport gate was reached: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("CW-M34 timed out waiting for the admitted writer to enter the gated CQL send")
	}

	if err := deleter.Session().Query(`
		DELETE FROM fs_objects USING TIMESTAMP ? WHERE library_id = ? AND fs_id = ?
	`, deleteTS, f.libraryID, fsID).Consistency(gocql.EachQuorum).Exec(); err != nil {
		t.Fatalf("CW-M34 GC tombstone at g=%d did not settle at EACH_QUORUM: %v", deleteTS, err)
	}

	gate.unblock()
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("CW-M34 old admitted write at w=%d did not return successful settlement: %v", writerTS, err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("CW-M34 old admitted write did not settle after releasing the transport gate")
	}

	var objType *string
	err := deleter.Session().Query(`
		SELECT obj_type FROM fs_objects WHERE library_id = ? AND fs_id = ?
	`, f.libraryID, fsID).Consistency(gocql.EachQuorum).Scan(&objType)
	if !errors.Is(err, gocql.ErrNotFound) || objType != nil {
		t.Fatalf("CW-M34 Cassandra behavior changed: successful old write must remain hidden by g (err=%v obj_type=%v)", err, objType)
	}
	t.Logf("CW-M34 reproduced on Cassandra 5.0.9: acknowledged materialization w=%d is hidden by already-acknowledged tombstone g=%d", writerTS, deleteTS)
}
