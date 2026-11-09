//go:build rehearsal

package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

func session(host, dc, user, pw string) *gocql.Session {
	c := gocql.NewCluster(host)
	c.Keyspace = "sesamefs"
	c.Authenticator = gocql.PasswordAuthenticator{Username: user, Password: pw}
	c.Timeout = 25 * time.Second
	c.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(gocql.DCAwareRoundRobinPolicy(dc))
	c.Consistency = gocql.LocalQuorum
	c.SerialConsistency = gocql.Serial
	s, err := c.CreateSession()
	if err != nil {
		fmt.Println("session error:", err)
		os.Exit(1)
	}
	return s
}

func report(name string, d []time.Duration, errs map[string]int, n int) {
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	p := func(q float64) time.Duration {
		if len(d) == 0 {
			return 0
		}
		return d[int(float64(len(d)-1)*q)].Round(time.Millisecond)
	}
	ne := 0
	for _, v := range errs {
		ne += v
	}
	fmt.Printf("%-34s n=%d err=%d p50=%v p95=%v p99=%v max=%v\n", name, n, ne, p(.5), p(.95), p(.99), p(1))
	for k, v := range errs {
		fmt.Printf("    %dx %s\n", v, k)
	}
}

func run(s *gocql.Session, name string, par, total int, f func(i int) error) {
	var mu sync.Mutex
	var d []time.Duration
	errs := map[string]int{}
	ch := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < par; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ch {
				t := time.Now()
				err := f(i)
				e := time.Since(t)
				mu.Lock()
				if err != nil {
					m := err.Error()
					if len(m) > 70 {
						m = m[:70]
					}
					errs[m]++
				} else {
					d = append(d, e)
				}
				mu.Unlock()
			}
		}()
	}
	for i := 0; i < total; i++ {
		ch <- i
	}
	close(ch)
	wg.Wait()
	report(name, d, errs, total)
}

func main() {
	pw := os.Getenv("APP_PW")
	user := os.Getenv("APP_USER")
	hosts := strings.Split(os.Getenv("HOSTS"), ",") // dc=ip
	ss := map[string]*gocql.Session{}
	var dcs []string
	for _, h := range hosts {
		kv := strings.SplitN(h, "=", 2)
		ss[kv[0]] = session(kv[1], kv[0], user, pw)
		dcs = append(dcs, kv[0])
	}
	s0 := ss[dcs[0]]
	if err := s0.Query(`CREATE TABLE IF NOT EXISTS probe (k text PRIMARY KEY, v int)`).Exec(); err != nil {
		fmt.Println("ddl:", err)
	}
	time.Sleep(5 * time.Second)
	rounds, _ := strconv.Atoi(os.Getenv("N"))
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	for _, dc := range dcs {
		s := ss[dc]
		for _, par := range []int{1, 6, 16} {
			pre := fmt.Sprintf("%s/%s/p%d", tag, dc, par)
			run(s, fmt.Sprintf("%s plain LOCAL_QUORUM par=%d", dc, par), par, rounds, func(i int) error {
				return s.Query(`INSERT INTO probe (k,v) VALUES (?,1)`, fmt.Sprintf("%s/q%d", pre, i)).Exec()
			})
			run(s, fmt.Sprintf("%s LWT SERIAL par=%d", dc, par), par, rounds, func(i int) error {
				_, err := s.Query(`INSERT INTO probe (k,v) VALUES (?,1) IF NOT EXISTS`, fmt.Sprintf("%s/s%d", pre, i)).SerialConsistency(gocql.Serial).ScanCAS()
				return err
			})
			run(s, fmt.Sprintf("%s LWT LOCAL_SERIAL par=%d", dc, par), par, rounds, func(i int) error {
				_, err := s.Query(`INSERT INTO probe (k,v) VALUES (?,1) IF NOT EXISTS`, fmt.Sprintf("%s/l%d", pre, i)).SerialConsistency(gocql.LocalSerial).ScanCAS()
				return err
			})
		}
	}
	// Visibility lag: LOCAL_QUORUM write in first DC, LOCAL_ONE poll in another.
	for _, dc := range dcs[1:] {
		var lags []time.Duration
		for i := 0; i < 50; i++ {
			k := fmt.Sprintf("%s/lag/%s/%d", tag, dc, i)
			if err := s0.Query(`INSERT INTO probe (k,v) VALUES (?,1)`, k).Exec(); err != nil {
				continue
			}
			t := time.Now()
			for time.Since(t) < 20*time.Second {
				var v int
				if err := ss[dc].Query(`SELECT v FROM probe WHERE k=?`, k).Consistency(gocql.LocalOne).Scan(&v); err == nil {
					break
				}
				time.Sleep(2 * time.Millisecond)
			}
			lags = append(lags, time.Since(t))
		}
		report(fmt.Sprintf("visibility lag %s->%s", dcs[0], dc), lags, map[string]int{}, len(lags))
	}
}
