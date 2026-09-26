package main

// Simulacion del inventario y de los parches de un agente (POST /api/v1/agent/inventory). Replica la cadencia y la
// logica de decision de agent/internal/inventory/runner.go: recolecta cada `interval`, envia completo si el rev cambio
// (o si es la primera vez, o si el server pidio need_full), y un latido con solo el rev cuando pasa `heartbeat` sin
// cambios. Todos los tiempos se dividen por el speedup.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const inventoryPath = "/api/v1/agent/inventory"

// invModule es la cadencia de un modulo (valores de buildModule en el agente).
type invModule struct {
	name      string
	interval  time.Duration // cada cuanto se recolecta
	heartbeat time.Duration // cada cuanto se refresca sin cambios
	start     time.Duration // la primera recoleccion espera un aleatorio en [0, start]
	spread    float64       // jitter (+-) sobre interval
	pChange   float64       // probabilidad de que el listado haya cambiado en una recoleccion
}

const minGap = 30 * time.Second // separacion minima entre dos envios completos del mismo modulo

// invOptions agrupa lo que el simulador de inventario necesita.
type invOptions struct {
	ports, services, processes, updates bool
	speedup                             int
	changeScale                         float64 // multiplica pChange (para provocar mas envios completos)
	stale                               float64 // probabilidad de un latido con rev viejo (provoca 409 need_full)
	warm                                bool    // arranca en regimen: ya envio el listado (version 0) y su ultimo envio es aleatorio dentro del latido
}

func (o invOptions) modules() []invModule {
	var ms []invModule
	if o.ports {
		ms = append(ms, invModule{"ports", time.Minute, 6 * time.Hour, 5 * time.Second, 0, 0.01})
	}
	if o.services {
		ms = append(ms, invModule{"services", 10 * time.Minute, 6 * time.Hour, 5 * time.Second, 0, 0.01})
	}
	if o.processes {
		ms = append(ms, invModule{"processes", 5 * time.Minute, 30 * time.Minute, 5 * time.Second, 0, 0.30})
	}
	if o.updates {
		ms = append(ms, invModule{"updates", 6 * time.Hour, 24 * time.Hour, 10 * time.Minute, 0.2, 0.05})
	}
	return ms
}

func revOf(host int, module string, version int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("loadgen|%d|%s|%d", host, module, version)))
	return hex.EncodeToString(h[:])
}

// itemsOf genera un listado valido para el server, determinista por (host, modulo, version).
func itemsOf(host int, module string, version int) any {
	r := rand.New(rand.NewSource(int64(host)*1_000_003 + int64(version)*7919 + int64(len(module))))
	switch module {
	case "ports":
		n := 8 + r.Intn(30)
		out := make([]map[string]any, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, map[string]any{"proto": "tcp", "bind": "0.0.0.0", "port": 1024 + i*7 + r.Intn(5), "proc": fmt.Sprintf("svc%d", i), "owner_known": true, "pid": 100 + i})
		}
		return out
	case "services":
		n := 60 + r.Intn(140)
		states := []string{"running", "stopped", "paused", "other"}
		starts := []string{"auto", "manual", "disabled", "other"}
		out := make([]map[string]any, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, map[string]any{"name": fmt.Sprintf("service-%03d", i), "display": fmt.Sprintf("Servicio de ejemplo %d", i), "state": states[r.Intn(4)], "start": starts[r.Intn(4)]})
		}
		return out
	case "processes":
		out := make([]map[string]any, 0, 100)
		for i := 0; i < 100; i++ {
			out = append(out, map[string]any{"name": fmt.Sprintf("proc-%03d", i), "count": 1 + r.Intn(8), "rss_bytes": int64(r.Intn(900)) << 20})
		}
		return out
	}
	sec := r.Intn(9)
	total := sec + r.Intn(50)
	adv := make([]string, sec)
	for i := range adv {
		adv[i] = fmt.Sprintf("USN-%d-%d", 5000+r.Intn(900), 1+r.Intn(3))
	}
	src := "apt"
	if host%5 == 0 {
		src = "wua"
	}
	return map[string]any{"pending_total": total, "pending_security": sec, "reboot_required": r.Intn(5) == 0,
		"last_installed": time.Now().AddDate(0, 0, -r.Intn(60)).Format("2006-01-02"), "source": src, "cache_age_s": r.Intn(86400), "advisories": adv}
}

// collector reune los resultados etiquetados de todos los agentes.
type collector struct {
	mu    sync.Mutex
	rs    []labeled
	start time.Time
}

type labeled struct {
	label string
	at    time.Duration
	result
}

func (c *collector) add(label string, r result) {
	c.mu.Lock()
	c.rs = append(c.rs, labeled{label: label, at: time.Since(c.start), result: r})
	c.mu.Unlock()
}

// invWorker es el estado de un modulo de un agente simulado. Un solo dueno: su goroutine.
type invWorker struct {
	host     int
	token    string
	mod      invModule
	o        invOptions
	client   *http.Client
	base     string
	col      *collector
	stop     *atomic.Bool
	version  int
	sent     bool
	lastRev  string
	lastSend time.Time
	needFull bool
	rng      *rand.Rand
}

// warmUp deja al agente como si ya hubiera enviado el listado de la version 0 hace un tiempo aleatorio (< latido).
func (w *invWorker) warmUp() {
	w.sent, w.lastRev = true, revOf(w.host, w.mod.name, 0)
	w.lastSend = time.Now().Add(-time.Duration(w.rng.Float64() * float64(w.scaled(w.mod.heartbeat))))
}

func (w *invWorker) scaled(d time.Duration) time.Duration { return d / time.Duration(w.o.speedup) }

func (w *invWorker) every() time.Duration {
	d := w.scaled(w.mod.interval)
	if w.mod.spread <= 0 {
		return d
	}
	span := float64(d) * w.mod.spread
	return time.Duration(float64(d) - span + w.rng.Float64()*2*span)
}

func (w *invWorker) loop() {
	wait := time.Duration(w.rng.Int63n(int64(w.scaled(w.mod.start)) + 1))
	for !w.stop.Load() {
		time.Sleep(wait)
		if w.stop.Load() {
			return
		}
		wait = w.step()
	}
}

func (w *invWorker) step() time.Duration {
	if w.sent && w.rng.Float64() < w.mod.pChange*w.o.changeScale {
		w.version++
	}
	rev := revOf(w.host, w.mod.name, w.version)
	now := time.Now()
	full := !w.sent || w.needFull || rev != w.lastRev
	switch {
	case full && w.sent && !w.needFull && now.Sub(w.lastSend) < w.scaled(minGap):
		return w.scaled(minGap) - now.Sub(w.lastSend)
	case !full && now.Sub(w.lastSend) < w.scaled(w.mod.heartbeat):
		return w.every()
	}
	kind := "full"
	body := map[string]any{"module": w.mod.name, "rev": rev}
	if full {
		body["collected_at"] = now.UTC().Format(time.RFC3339)
		body["items"] = itemsOf(w.host, w.mod.name, w.version)
		body["truncated"] = false
	} else {
		kind = "heartbeat"
		if w.rng.Float64() < w.o.stale {
			body["rev"] = revOf(w.host, w.mod.name, -1) // el server no tiene este rev: 409 need_full
			kind = "heartbeat-stale"
		}
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", w.base+inventoryPath, strings.NewReader(string(b)))
	req.Header.Set("Authorization", "Bearer "+w.token)
	req.Header.Set("Content-Type", "application/json")
	r := do(w.client, req)
	w.col.add(w.mod.name+" "+kind, r)
	switch {
	case r.status >= 200 && r.status <= 299:
		w.sent, w.lastRev, w.lastSend, w.needFull = true, rev, now, false
		return w.every()
	case r.status == http.StatusConflict && !full:
		w.needFull = true
		return w.scaled(time.Second)
	default: // 429, 5xx, red: backoff base del agente (30 s)
		return w.scaled(30 * time.Second)
	}
}
