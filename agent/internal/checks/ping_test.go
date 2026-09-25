package checks

import (
	"context"
	"errors"
	"net/netip"
	"strconv"
	"sync"
	"testing"
	"time"
)

// fakePinger cuenta llamadas y concurrencia, y responde con fn.
type fakePinger struct {
	fn    func(ip netip.Addr) (time.Duration, error)
	delay time.Duration

	mu      sync.Mutex
	got     []netip.Addr
	cur     int
	maxSeen int
}

func (f *fakePinger) Ping(ctx context.Context, ip netip.Addr) (time.Duration, error) {
	f.mu.Lock()
	f.got = append(f.got, ip)
	f.cur++
	f.maxSeen = max(f.maxSeen, f.cur)
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.cur--
		f.mu.Unlock()
	}()
	select {
	case <-time.After(f.delay):
	case <-ctx.Done():
		return 0, ErrPingNoReply
	}
	return f.fn(ip)
}

func (f *fakePinger) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.got)
}

func mustProbePolicy(t *testing.T, raw string) *ProbePolicy {
	t.Helper()
	p, err := ParseProbePolicy(raw, DefaultProbeMaxTargets)
	if err != nil {
		t.Fatalf("ParseProbePolicy(%q): %v", raw, err)
	}
	return p
}

func pingCheck(id, target string) Check {
	return Check{ID: id, Type: TypePing, Name: "ping " + id, Target: target, IntervalS: 3600, TimeoutMS: 2000, WarnMS: 100, CritMS: 500}
}

func okPinger(rtt time.Duration) *fakePinger {
	return &fakePinger{fn: func(netip.Addr) (time.Duration, error) { return rtt, nil }}
}

func TestPingProbeOutcomes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		allow      string
		target     string
		pinger     *fakePinger
		wantStatus string
		wantDetail string
		wantCalls  int
	}{
		{"responde", "192.168.1.0/24", "192.168.1.10", okPinger(20 * time.Millisecond), StatusUp, DetailOK, 1},
		{"warn por latencia", "192.168.1.0/24", "192.168.1.10", okPinger(150 * time.Millisecond), StatusWarn, DetailOK, 1},
		{"crit por latencia", "192.168.1.0/24", "192.168.1.10", okPinger(600 * time.Millisecond), StatusDown, DetailOK, 1},
		{"sin respuesta", "192.168.1.0/24", "192.168.1.10",
			&fakePinger{fn: func(netip.Addr) (time.Duration, error) { return 0, ErrPingNoReply }}, StatusDown, DetailTimeout, 1},
		{"sin camino disponible", "192.168.1.0/24", "192.168.1.10",
			&fakePinger{fn: func(netip.Addr) (time.Duration, error) { return 0, ErrPingUnavailable }}, StatusUnknown, DetailUnsupported, 1},
		{"vacio = desactivado", "", "192.168.1.10", okPinger(time.Millisecond), StatusUnknown, DetailBlocked, 0},
		{"fuera de la allowlist", "192.168.1.0/24", "192.168.2.10", okPinger(time.Millisecond), StatusUnknown, DetailBlocked, 0},
		{"loopback", "192.168.1.0/24", "127.0.0.1", okPinger(time.Millisecond), StatusUnknown, DetailBlocked, 0},
		{"metadata", "192.168.1.0/24", "169.254.169.254", okPinger(time.Millisecond), StatusUnknown, DetailBlocked, 0},
		{"ipv4-mapped a loopback", "192.168.1.0/24", "::ffff:127.0.0.1", okPinger(time.Millisecond), StatusUnknown, DetailBlocked, 0},
		{"hostname", "192.168.1.0/24", "router.local", okPinger(time.Millisecond), StatusUnknown, DetailUnsupported, 0},
		{"inyeccion con ;", "192.168.1.0/24", "192.168.1.10;reboot", okPinger(time.Millisecond), StatusUnknown, DetailUnsupported, 0},
		{"inyeccion con espacio", "192.168.1.0/24", "192.168.1.10 -f", okPinger(time.Millisecond), StatusUnknown, DetailUnsupported, 0},
		{"opcion como objetivo", "192.168.1.0/24", "-f", okPinger(time.Millisecond), StatusUnknown, DetailUnsupported, 0},
		{"objetivo vacio", "192.168.1.0/24", "", okPinger(time.Millisecond), StatusUnknown, DetailUnsupported, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newTestRunner(t, Options{ProbeAllow: mustProbePolicy(t, tt.allow), Pinger: tt.pinger})
			r.Apply(t.Context(), Set{Rev: "r1", Items: []Check{pingCheck("p1", tt.target)}})
			res := waitResults(t, r, 1, 5*time.Second)[0]
			if res.Status != tt.wantStatus || res.Detail != tt.wantDetail {
				t.Errorf("got %s/%s, want %s/%s", res.Status, res.Detail, tt.wantStatus, tt.wantDetail)
			}
			if res.CheckID != "p1" {
				t.Errorf("check_id = %q, want p1", res.CheckID)
			}
			if got := tt.pinger.calls(); got != tt.wantCalls {
				t.Errorf("llamadas al pinger = %d, want %d", got, tt.wantCalls)
			}
		})
	}
}

func TestPingRetriesOnlyWhenDown(t *testing.T) {
	t.Parallel()
	n := 0
	p := &fakePinger{fn: func(netip.Addr) (time.Duration, error) {
		n++ // el ciclo de un chequeo es secuencial
		if n < 3 {
			return 0, ErrPingNoReply
		}
		return time.Millisecond, nil
	}}
	r := newTestRunner(t, Options{ProbeAllow: mustProbePolicy(t, "192.168.1.0/24"), Pinger: p})
	c := pingCheck("p1", "192.168.1.10")
	c.Retries = 2
	r.Apply(t.Context(), Set{Rev: "r1", Items: []Check{c}})
	if res := waitResults(t, r, 1, 5*time.Second)[0]; res.Status != StatusUp {
		t.Errorf("status = %s, want up tras 2 reintentos", res.Status)
	}
	if got := p.calls(); got != 3 {
		t.Errorf("llamadas = %d, want 3", got)
	}
}

func TestPingTimeoutIsBoundedByCheck(t *testing.T) {
	t.Parallel()
	// El pinger nunca contesta: el plazo del chequeo (min 200 ms) lo corta.
	p := &fakePinger{delay: time.Hour, fn: func(netip.Addr) (time.Duration, error) { return time.Millisecond, nil }}
	r := newTestRunner(t, Options{ProbeAllow: mustProbePolicy(t, "192.168.1.0/24"), Pinger: p})
	c := pingCheck("p1", "192.168.1.10")
	c.TimeoutMS = 1 // se sube al piso de 200 ms
	start := time.Now()
	r.Apply(t.Context(), Set{Rev: "r1", Items: []Check{c}})
	res := waitResults(t, r, 1, 5*time.Second)[0]
	if res.Status != StatusDown || res.Detail != DetailTimeout {
		t.Errorf("got %s/%s, want down/timeout", res.Status, res.Detail)
	}
	if el := time.Since(start); el > 3*time.Second {
		t.Errorf("tardo %s, el timeout debia cortar a ~200 ms", el)
	}
}

func TestPingConcurrencyIsBounded(t *testing.T) {
	t.Parallel()
	p := &fakePinger{delay: 100 * time.Millisecond, fn: func(netip.Addr) (time.Duration, error) { return time.Millisecond, nil }}
	r := newTestRunner(t, Options{ProbeAllow: mustProbePolicy(t, "192.168.1.0/24"), Pinger: p, MaxChecks: 60})
	items := make([]Check, 40)
	for i := range items {
		items[i] = pingCheck("p"+strconv.Itoa(i), "192.168.1."+strconv.Itoa(i+1))
	}
	r.Apply(t.Context(), Set{Rev: "r1", Items: items})
	waitResults(t, r, 40, 10*time.Second)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.maxSeen > MaxPingConcurrent {
		t.Errorf("pings simultaneos = %d, want <= %d", p.maxSeen, MaxPingConcurrent)
	}
	if p.maxSeen < 2 {
		t.Errorf("pings simultaneos = %d, se esperaba paralelismo", p.maxSeen)
	}
}

func TestOneFailureDoesNotStopTheCycle(t *testing.T) {
	t.Parallel()
	p := &fakePinger{fn: func(ip netip.Addr) (time.Duration, error) {
		if ip == netip.MustParseAddr("192.168.1.2") {
			return 0, ErrPingNoReply
		}
		return time.Millisecond, nil
	}}
	r := newTestRunner(t, Options{ProbeAllow: mustProbePolicy(t, "192.168.1.0/24"), Pinger: p})
	r.Apply(t.Context(), Set{Rev: "r1", Items: []Check{pingCheck("a", "192.168.1.1"), pingCheck("b", "192.168.1.2"), pingCheck("c", "192.168.1.3")}})
	got := map[string]string{}
	for _, res := range waitResults(t, r, 3, 5*time.Second) {
		got[res.CheckID] = res.Status
	}
	want := map[string]string{"a": StatusUp, "b": StatusDown, "c": StatusUp}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("chequeo %s: %s, want %s", id, got[id], w)
		}
	}
}

func TestMaxChecksIsExplicit(t *testing.T) {
	t.Parallel()
	p := okPinger(time.Millisecond)
	r := newTestRunner(t, Options{ProbeAllow: mustProbePolicy(t, "192.168.1.0/24"), Pinger: p, MaxChecks: 30})
	items := make([]Check, 25)
	for i := range items {
		items[i] = pingCheck("p"+strconv.Itoa(i), "192.168.1."+strconv.Itoa(i+1))
	}
	r.Apply(t.Context(), Set{Rev: "r1", Items: items})
	for _, res := range waitResults(t, r, 25, 5*time.Second) {
		if res.Status != StatusUp {
			t.Errorf("chequeo %s: %s/%s, con MaxChecks=30 los 25 deben ejecutarse", res.CheckID, res.Status, res.Detail)
		}
	}
}

func TestChainPingerFallsThroughOnlyWhenUnavailable(t *testing.T) {
	t.Parallel()
	ip := netip.MustParseAddr("192.168.1.10")
	unavailable := &fakePinger{fn: func(netip.Addr) (time.Duration, error) { return 0, ErrPingUnavailable }}
	noReply := &fakePinger{fn: func(netip.Addr) (time.Duration, error) { return 0, ErrPingNoReply }}
	ok := okPinger(5 * time.Millisecond)

	if rtt, err := (chainPinger{unavailable, ok}).Ping(t.Context(), ip); err != nil || rtt != 5*time.Millisecond {
		t.Errorf("unavailable->ok: %v, %v", rtt, err)
	}
	if _, err := (chainPinger{noReply, ok}).Ping(t.Context(), ip); !errors.Is(err, ErrPingNoReply) {
		t.Errorf("noReply no debe pasar al siguiente camino: %v", err)
	}
	if ok.calls() != 1 {
		t.Errorf("el segundo camino se uso %d veces, want 1", ok.calls())
	}
	if _, err := (chainPinger{unavailable}).Ping(t.Context(), ip); !errors.Is(err, ErrPingUnavailable) {
		t.Errorf("todos no disponibles: %v, want ErrPingUnavailable", err)
	}
}
