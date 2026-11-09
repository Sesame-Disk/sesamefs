//go:build rehearsal

// Raw Cassandra contention probe: compare-and-set on ONE partition from three DCs, the
// shape of the per-library upload lease and HEAD CAS, with no SesameFS involved.
package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

func main() {
	user, pw := os.Getenv("APP_USER"), os.Getenv("APP_PW")
	var ss []*gocql.Session
	var dcs []string
	for _, h := range strings.Split(os.Getenv("HOSTS"), ",") {
		kv := strings.SplitN(h, "=", 2)
		c := gocql.NewCluster(kv[1])
		c.Keyspace = "sesamefs"
		c.Authenticator = gocql.PasswordAuthenticator{Username: user, Password: pw}
		c.Timeout = 25 * time.Second
		c.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(gocql.DCAwareRoundRobinPolicy(kv[0]))
		c.Consistency = gocql.LocalQuorum
		c.SerialConsistency = gocql.Serial
		s, err := c.CreateSession()
		if err != nil {
			fmt.Println("session:", err)
			os.Exit(1)
		}
		ss = append(ss, s)
		dcs = append(dcs, kv[0])
	}
	if err := ss[0].Query(`CREATE TABLE IF NOT EXISTS probe2 (k text PRIMARY KEY, v int)`).Exec(); err != nil {
		fmt.Println("ddl:", err)
	}
	time.Sleep(6 * time.Second)
	secs, _ := strconv.Atoi(os.Getenv("SECS"))
	for _, perDC := range []int{1, 2, 5} {
		key := fmt.Sprintf("k-%d-%d", perDC, time.Now().UnixNano())
		// warm the partition/table outside the measured window
		for attempt := 0; attempt < 5; attempt++ {
			if _, err := ss[0].Query(`INSERT INTO probe2 (k,v) VALUES (?,0) IF NOT EXISTS`, key).ScanCAS(); err == nil {
				break
			}
			time.Sleep(time.Second)
		}
		var mu sync.Mutex
		var lat []time.Duration
		applied, rejected := 0, 0
		errs := map[string]int{}
		end := time.Now().Add(time.Duration(secs) * time.Second)
		var wg sync.WaitGroup
		for _, s := range ss {
			for w := 0; w < perDC; w++ {
				wg.Add(1)
				go func(s *gocql.Session) {
					defer wg.Done()
					for time.Now().Before(end) {
						var cur int
						if err := s.Query(`SELECT v FROM probe2 WHERE k=?`, key).Consistency(gocql.LocalQuorum).Scan(&cur); err != nil && !errors.Is(err, gocql.ErrNotFound) {
							mu.Lock()
							errs["read: "+trim(err)]++
							mu.Unlock()
							continue
						}
						t := time.Now()
						var old int
						ok, err := s.Query(`UPDATE probe2 SET v=? WHERE k=? IF v=?`, cur+1, key, cur).ScanCAS(&old)
						e := time.Since(t)
						mu.Lock()
						switch {
						case err != nil:
							errs[trim(err)]++
						case ok:
							applied++
							lat = append(lat, e)
						default:
							rejected++
							lat = append(lat, e)
						}
						mu.Unlock()
					}
				}(s)
			}
		}
		wg.Wait()
		sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
		q := func(p float64) time.Duration {
			if len(lat) == 0 {
				return 0
			}
			return lat[int(float64(len(lat)-1)*p)].Round(time.Millisecond)
		}
		ne := 0
		for _, n := range errs {
			ne += n
		}
		fmt.Printf("writers=%dx%dDC applied=%d (%.1f/s) rejected=%d errors=%d p50=%v p95=%v max=%v\n", perDC, len(ss), applied, float64(applied)/float64(secs), rejected, ne, q(.5), q(.95), q(1))
		for k, n := range errs {
			fmt.Printf("    %dx %s\n", n, k)
		}
	}
}

func trim(err error) string {
	m := err.Error()
	if len(m) > 72 {
		m = m[:72]
	}
	return m
}
