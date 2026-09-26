// Generador de carga de argos (solo stdlib): mide latencia de los endpoints del dashboard y simula agentes reportando.
//
//	go run . -base http://127.0.0.1:18082 -user perf -pass CLAVE -hosts 1000 -agents-for 60s
//
// Solo contra un server de pruebas con la BD sintetica (scripts/perf/seed.sql). Los tokens de agente son
// perf-token-NNNN. La clave se pasa por flag: no lo uses contra un server real.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type result struct {
	lat    time.Duration
	status int
	bytes  int
}

func pct(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	return d[int(float64(len(d)-1)*p)]
}

func summarize(name string, rs []result, wall time.Duration) {
	lats := make([]time.Duration, 0, len(rs))
	codes := map[int]int{}
	var total int
	for _, r := range rs {
		lats = append(lats, r.lat)
		codes[r.status]++
		total += r.bytes
	}
	sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })
	var cs []string
	for c, n := range codes {
		cs = append(cs, fmt.Sprintf("%dx%d", c, n))
	}
	sort.Strings(cs)
	rps := float64(len(rs)) / wall.Seconds()
	r := func(d time.Duration) time.Duration { return d.Round(100 * time.Microsecond) }
	fmt.Printf("| %s | %d | %.1f | %s | %s | %s | %s | %d KB | %s |\n", name, len(rs), rps,
		r(pct(lats, .5)), r(pct(lats, .95)), r(pct(lats, .99)), r(lats[len(lats)-1]), total/len(rs)/1024, strings.Join(cs, " "))
}

func main() {
	base := flag.String("base", "http://127.0.0.1:18082", "URL del server de pruebas")
	user := flag.String("user", "perf", "usuario admin")
	pass := flag.String("pass", "", "clave del admin")
	hosts := flag.Int("hosts", 1000, "hosts sembrados (tokens perf-token-0001..N)")
	reqs := flag.Int("n", 30, "solicitudes por endpoint y por nivel de concurrencia")
	agentsFor := flag.Duration("agents-for", 0, "si > 0, simula agentes reportando durante este tiempo")
	interval := flag.Duration("agent-interval", 30*time.Second, "intervalo de cada agente simulado")
	speedup := flag.Int("speedup", 1, "multiplica la tasa de reporte (los agentes reales van cada 30 s)")
	skipRead := flag.Bool("skip-read", false, "no medir los endpoints de lectura")
	only := flag.String("only", "", "solo endpoints que contengan alguna de estas subcadenas (separadas por coma)")
	flag.Parse()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 60 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 256}}

	if !*skipRead {
		if *pass == "" {
			fmt.Fprintln(os.Stderr, "falta -pass")
			os.Exit(2)
		}
		login(client, *base, *user, *pass)
		ids := hostIDs(client, *base, 40)
		fmt.Printf("\nLectura (%d solicitudes por endpoint y nivel; c = concurrencia)\n\n", *reqs)
		fmt.Println("| endpoint | n | req/s | p50 | p95 | p99 | max | tamano | codigos |")
		fmt.Println("|---|---|---|---|---|---|---|---|---|")
		eps := endpoints(ids)
		for _, c := range []int{1, 8} {
			for _, ep := range eps {
				if !matches(ep, *only) {
					continue
				}
				rs, wall := run(client, *base+ep, *reqs, c)
				summarize(fmt.Sprintf("c=%d %s", c, ep), rs, wall)
			}
		}
	}
	if *agentsFor > 0 {
		agents(*base, *hosts, *agentsFor, *interval, *speedup)
	}
}

func matches(ep, only string) bool {
	if only == "" {
		return true
	}
	for _, sub := range strings.Split(only, ",") {
		if strings.Contains(ep, sub) {
			return true
		}
	}
	return false
}

func endpoints(ids []string) []string {
	r := rand.New(rand.NewSource(1))
	pick := func() string { return ids[r.Intn(len(ids))] }
	e := []string{
		"/health",
		"/api/v1/summary",
		"/api/v1/hosts?limit=15",
		"/api/v1/hosts?limit=100",
		"/api/v1/hosts?limit=15&sort=cpu&dir=desc",
		"/api/v1/hosts?limit=15&sort=mem&dir=desc",
		"/api/v1/hosts?limit=15&sort=disk&dir=desc",
		"/api/v1/hosts?limit=15&sort=severity",
		"/api/v1/hosts?limit=15&q=perf-host-05",
		"/api/v1/hosts?limit=15&status=down",
		"/api/v1/alerts?limit=15",
		"/api/v1/alerts/summary",
		"/api/v1/alert-rules",
		"/api/v1/updates?limit=15",
		"/api/v1/updates?limit=15&security=true&sort=security&dir=desc",
		"/api/v1/groups",
		"/api/v1/reports/availability?range=7d",
		"/api/v1/reports/availability?range=30d&by=group",
		"/api/v1/reports/availability.csv?range=7d",
	}
	for _, rng := range []string{"24h", "7d", "30d"} {
		e = append(e, fmt.Sprintf("/api/v1/hosts/%s/metrics?range=%s", pick(), rng))
	}
	return append(e,
		"/api/v1/hosts/"+pick(),
		"/api/v1/hosts/"+pick()+"/events",
		"/api/v1/hosts/"+pick()+"/inventory/ports",
		"/api/v1/hosts/"+pick()+"/updates",
	)
}

func login(c *http.Client, base, user, pass string) {
	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	req, _ := http.NewRequest("POST", base+"/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil || resp.StatusCode != 200 {
		fmt.Fprintln(os.Stderr, "login fallido:", err, resp)
		os.Exit(1)
	}
	_ = resp.Body.Close()
}

func hostIDs(c *http.Client, base string, n int) []string {
	resp, err := c.Get(base + fmt.Sprintf("/api/v1/hosts?limit=%d", n))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	var out struct {
		Data struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || len(out.Data.Items) == 0 {
		fmt.Fprintln(os.Stderr, "sin hosts en la lista:", err)
		os.Exit(1)
	}
	ids := make([]string, len(out.Data.Items))
	for i, it := range out.Data.Items {
		ids[i] = it.ID
	}
	return ids
}

func do(c *http.Client, req *http.Request) result {
	t := time.Now()
	resp, err := c.Do(req)
	r := result{lat: time.Since(t)}
	if err != nil {
		r.status = -1
		return r
	}
	b, _ := io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	r.status, r.bytes = resp.StatusCode, int(b)
	return r
}

func run(c *http.Client, url string, n, conc int) ([]result, time.Duration) {
	var mu sync.Mutex
	var rs []result
	jobs := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		jobs <- struct{}{}
	}
	close(jobs)
	start := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < conc; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				req, _ := http.NewRequest("GET", url, nil)
				r := do(c, req)
				mu.Lock()
				rs = append(rs, r)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return rs, time.Since(start)
}

// agents simula `hosts` agentes: cada uno reporta cada interval/speedup (con desfase aleatorio) durante d.
func agents(base string, hosts int, d, interval time.Duration, speedup int) {
	step := interval / time.Duration(speedup)
	fmt.Printf("\nIngest: %d agentes, cada %s (x%d = %.1f req/s objetivo), %s\n\n", hosts, step, speedup, float64(hosts)/step.Seconds(), d)
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 512}}
	var mu sync.Mutex
	var rs []result
	var stop atomic.Bool
	var wg sync.WaitGroup
	start := time.Now()
	for i := 1; i <= hosts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			token := fmt.Sprintf("perf-token-%04d", i)
			time.Sleep(time.Duration(rand.Int63n(int64(step))))
			for n := 0; !stop.Load(); n++ {
				body := fmt.Sprintf(`{"timestamp":%d,"metrics":{"cpu_pct":%.1f,"mem_used_mb":%d,"mem_total_mb":16384,"net":{"rx_bytes":%d,"tx_bytes":%d}},"disks":[{"mount":"/","used_mb":%d,"total_mb":500000},{"mount":"/data","used_mb":%d,"total_mb":500000}]}`,
					time.Now().Unix(), rand.Float64()*100, 2000+rand.Intn(6000), n*10000, n*4000, 100000+rand.Intn(200000), 50000+rand.Intn(200000))
				req, _ := http.NewRequest("POST", base+"/api/v1/agent/metrics", strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				r := do(client, req)
				mu.Lock()
				rs = append(rs, r)
				mu.Unlock()
				time.Sleep(step)
			}
		}(i)
	}
	time.Sleep(d)
	stop.Store(true)
	wg.Wait()
	fmt.Println("| endpoint | n | req/s | p50 | p95 | p99 | max | tamano | codigos |")
	fmt.Println("|---|---|---|---|---|---|---|---|---|")
	summarize("POST /api/v1/agent/metrics", rs, time.Since(start))
}
