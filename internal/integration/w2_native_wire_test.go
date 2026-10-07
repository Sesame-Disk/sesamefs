//go:build integration

package integration

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sesame-Disk/sesamefs/internal/config"
	dbpkg "github.com/Sesame-Disk/sesamefs/internal/db"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// w2WireProxy forwards native v4 frames unchanged. It loses a real HEAD CAS
// request or response; it never fabricates a Cassandra result or driver error.
// Only this dedicated writer session traverses it. Verification bypasses it.
type w2WireProxy struct {
	listener         net.Listener
	upstream         string
	mu               sync.Mutex
	conns            map[net.Conn]bool
	closed           bool
	armed            bool
	beforeServer     bool
	hideConfirmation bool
	dropQuery        string // E1-11: native loss of one SELECT family on this session only.
	blackout         bool
	dropped          int
	reached          chan struct{}
	once             sync.Once
	wg               sync.WaitGroup
	hold             string
	release          chan struct{}
	resumeOnce       sync.Once
}

type w2CQLFrame struct {
	header [9]byte
	body   []byte
}

func w2ReadFrame(r io.Reader) (w2CQLFrame, error) {
	var f w2CQLFrame
	if _, err := io.ReadFull(r, f.header[:]); err != nil {
		return f, err
	}
	if f.header[0]&0x7f != 4 || f.header[1]&1 != 0 {
		return f, fmt.Errorf("evidence proxy requires uncompressed native v4")
	}
	n := binary.BigEndian.Uint32(f.header[5:9])
	if n > 16*1024*1024 {
		return f, fmt.Errorf("oversized evidence frame: %d", n)
	}
	f.body = make([]byte, n)
	_, err := io.ReadFull(r, f.body)
	return f, err
}
func (f w2CQLFrame) stream() uint16 { return binary.BigEndian.Uint16(f.header[2:4]) }
func (f w2CQLFrame) write(w io.Writer) error {
	data := append(f.header[:], f.body...)
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
func w2LongString(b []byte) string {
	if len(b) < 4 {
		return ""
	}
	n := int(binary.BigEndian.Uint32(b[:4]))
	if n > len(b)-4 {
		return ""
	}
	return string(b[4 : 4+n])
}
func w2ShortBytes(b []byte) string {
	if len(b) < 2 {
		return ""
	}
	n := int(binary.BigEndian.Uint16(b[:2]))
	if n > len(b)-2 {
		return ""
	}
	return string(b[2 : 2+n])
}
func w2IsHeadCAS(stmt string) bool {
	s := strings.Join(strings.Fields(strings.ToLower(stmt)), " ")
	return strings.HasPrefix(s, "update libraries set head_commit_id =") && strings.Contains(s, "if head_commit_id =")
}
func newW2WireProxy(t *testing.T) *w2WireProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &w2WireProxy{listener: ln, upstream: splitEnvOrDefault("CASSANDRA_HOSTS", "cassandra:9042")[0], conns: map[net.Conn]bool{}, reached: make(chan struct{}), release: make(chan struct{})}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			p.mu.Lock()
			if p.closed {
				p.mu.Unlock()
				c.Close()
				return
			}
			p.conns[c] = true
			p.wg.Add(1)
			p.mu.Unlock()
			go p.serve(c)
		}
	}()
	t.Cleanup(func() {
		p.resume()
		p.mu.Lock()
		p.closed = true
		ln.Close()
		for c := range p.conns {
			c.Close()
		}
		p.mu.Unlock()
		p.wg.Wait()
	})
	return p
}
func (p *w2WireProxy) arm(beforeServer, hideConfirmation bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.armed = true
	p.beforeServer = beforeServer
	p.hideConfirmation = hideConfirmation
}
func (p *w2WireProxy) restore() { p.mu.Lock(); p.armed = false; p.blackout = false; p.mu.Unlock() }
func (p *w2WireProxy) lose() {
	p.mu.Lock()
	p.dropped++
	p.blackout = p.hideConfirmation
	p.mu.Unlock()
	p.once.Do(func() { close(p.reached) })
}
func (p *w2WireProxy) serve(client net.Conn) {
	defer p.wg.Done()
	defer client.Close()
	upstream, err := net.DialTimeout("tcp", p.upstream, 10*time.Second)
	if err != nil {
		return
	}
	defer upstream.Close()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.conns[upstream] = true
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.conns, client); delete(p.conns, upstream); p.mu.Unlock() }()
	type request struct {
		stmt          string
		prepare, lose bool
	}
	var mu sync.Mutex
	pending := map[uint16]request{}
	prepared := map[string]string{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer upstream.Close()
		defer client.Close()
		for {
			f, err := w2ReadFrame(client)
			if err != nil {
				return
			}
			var req request
			mu.Lock()
			switch f.header[4] {
			case 7:
				req.stmt = w2LongString(f.body)
			case 9:
				req.stmt = w2LongString(f.body)
				req.prepare = true
			case 10:
				req.stmt = prepared[w2ShortBytes(f.body)]
			}
			mu.Unlock()
			p.mu.Lock()
			blackout := p.blackout
			target := p.armed && !req.prepare && (w2IsHeadCAS(req.stmt) || (p.dropQuery != "" && strings.Contains(strings.ToLower(req.stmt), p.dropQuery)))
			before := p.beforeServer
			hold := p.hold != "" && !req.prepare && strings.Contains(strings.ToLower(req.stmt), p.hold)
			p.mu.Unlock()
			if hold {
				p.once.Do(func() { close(p.reached) })
				<-p.release
			}
			if blackout && (f.header[4] == 7 || f.header[4] == 10) {
				continue
			}
			if target && before {
				p.lose()
				continue
			}
			req.lose = target
			mu.Lock()
			pending[f.stream()] = req
			mu.Unlock()
			if err := f.write(upstream); err != nil {
				return
			}
		}
	}()
	for {
		f, err := w2ReadFrame(upstream)
		if err != nil {
			break
		}
		mu.Lock()
		req := pending[f.stream()]
		delete(pending, f.stream())
		if req.prepare && f.header[4] == 8 && len(f.body) >= 6 && binary.BigEndian.Uint32(f.body[:4]) == 4 {
			prepared[w2ShortBytes(f.body[4:])] = req.stmt
		}
		mu.Unlock()
		if req.lose {
			p.lose()
			continue
		}
		if err := f.write(client); err != nil {
			break
		}
	}
	client.Close()
	upstream.Close()
	<-done
}

func w2EvidenceSession(t *testing.T, endpoint string, observer gocql.QueryObserver) *dbpkg.DB {
	t.Helper()
	cluster := gocql.NewCluster(endpoint)
	cluster.Keyspace = envOrDefault("CASSANDRA_KEYSPACE", "sesamefs")
	cluster.ProtoVersion = 4
	cluster.DisableInitialHostLookup = true
	cluster.NumConns = 1
	cluster.Consistency = gocql.LocalQuorum
	cluster.SerialConsistency = gocql.Serial
	cluster.Timeout = 1500 * time.Millisecond
	cluster.ConnectTimeout = 10 * time.Second
	cluster.QueryObserver = observer
	if u, p := os.Getenv("CASSANDRA_USERNAME"), os.Getenv("CASSANDRA_PASSWORD"); u != "" && p != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{Username: u, Password: p}
	}
	session, err := cluster.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	return dbpkg.NewPublicationEvidenceDB(session, config.DatabaseConfig{Hosts: []string{endpoint}, Keyspace: cluster.Keyspace, LocalDC: envOrDefault("CASSANDRA_LOCAL_DC", "datacenter1"), Consistency: "LOCAL_QUORUM", SerialConsistency: "SERIAL"})
}

type w2HeadErrorObserver struct {
	mu     sync.Mutex
	errors []error
}

func (o *w2HeadErrorObserver) ObserveQuery(_ context.Context, q gocql.ObservedQuery) {
	if w2IsHeadCAS(q.Statement) && q.Err != nil {
		o.mu.Lock()
		o.errors = append(o.errors, q.Err)
		o.mu.Unlock()
	}
}

func (p *w2WireProxy) armHold(statement string) {
	p.mu.Lock()
	p.hold = strings.ToLower(statement)
	p.mu.Unlock()
}
func (p *w2WireProxy) resume() { p.resumeOnce.Do(func() { close(p.release) }) }
