package checks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

var (
	loopback = netip.MustParseAddr("127.0.0.1")
	v2cOK    = SNMPParams{Version: 2, Community: "s3cr3t-comm"}
	v3OK     = SNMPParams{Version: 3, User: "argos", Level: "authPriv", AuthProto: "SHA256",
		AuthPass: "auth-pass-XYZ", PrivProto: "AES128", PrivPass: "priv-pass-XYZ"}
)

func snmpReq(p SNMPParams, oid string) snmpRequest {
	return snmpRequest{IP: loopback, Params: p, OID: oid, Timeout: 300 * time.Millisecond}
}

// oidBytes codifica un OID con puntos como los bytes BER de su contenido.
func oidBytes(t *testing.T, oid string) []byte {
	t.Helper()
	var n []int
	for _, p := range strings.Split(oid, ".") {
		v, err := strconv.Atoi(p)
		if err != nil {
			t.Fatalf("oid %q: %v", oid, err)
		}
		n = append(n, v)
	}
	out := []byte{byte(n[0]*40 + n[1])}
	for _, v := range n[2:] {
		enc := []byte{byte(v & 0x7f)}
		for v >>= 7; v > 0; v >>= 7 {
			enc = append([]byte{byte(v&0x7f | 0x80)}, enc...)
		}
		out = append(out, enc...)
	}
	return out
}

func TestSNMPClientV2cAgainstSimulator(t *testing.T) {
	t.Parallel()
	ifOID := oidIfOperStatusP + "7"
	tests := []struct {
		name    string
		oid     string
		value   []byte
		want    int64
		wantErr error // nil = exito; ErrSNMPNoSuchObject; o errGeneric
	}{
		{"reachable timeticks", oidSysUpTime, tlv(0x43, []byte{0x00, 0x00, 0x30, 0x39}), 12345, nil},
		{"if_oper up", ifOID, tlv(0x02, []byte{1}), 1, nil},
		{"if_oper dormant", ifOID, tlv(0x02, []byte{5}), 5, nil},
		{"no such instance", ifOID, []byte{0x81, 0x00}, 0, ErrSNMPNoSuchObject},
		{"no such object", ifOID, []byte{0x80, 0x00}, 0, ErrSNMPNoSuchObject},
		{"valor no entero", ifOID, tlv(0x04, []byte("x")), 0, errGeneric},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sim := startSNMPSim(t, func(pkt []byte) []byte {
				comm, id, oid, ok := v2cRequest(pkt)
				if !ok || string(comm) != v2cOK.Community || !bytes.Equal(oid, oidBytes(t, tc.oid)) {
					return nil
				}
				return v2cResponse(comm, id, oid, tc.value)
			})
			c := &gosnmpClient{port: sim.port}
			got, err := c.Get(context.Background(), snmpReq(v2cOK, tc.oid))
			switch {
			case tc.wantErr == nil:
				if err != nil || got != tc.want {
					t.Fatalf("got %d, %v; want %d", got, err, tc.want)
				}
			case errors.Is(tc.wantErr, ErrSNMPNoSuchObject):
				if !errors.Is(err, ErrSNMPNoSuchObject) {
					t.Fatalf("err = %v, want no such object", err)
				}
			default:
				if err == nil || errors.Is(err, ErrSNMPTimeout) || errors.Is(err, ErrSNMPNoSuchObject) {
					t.Fatalf("err = %v, want error generico", err)
				}
			}
		})
	}
}

var errGeneric = errors.New("generico")

func TestSNMPClientOversizedDatagramIsDiscarded(t *testing.T) {
	t.Parallel()
	// 65507 bytes es el maximo de un datagrama UDP/IPv4; basura, nunca un GET valido.
	sim := startSNMPSim(t, func([]byte) []byte { return bytes.Repeat([]byte{0x30}, 65507) })
	c := &gosnmpClient{port: sim.port}
	if v, err := c.Get(context.Background(), snmpReq(v2cOK, oidSysUpTime)); err == nil {
		t.Fatalf("respuesta gigante aceptada: %d", v)
	}
	if got := mapSNMPError(context.Background(), errors.New("response buffer too small")); !errors.Is(got, ErrSNMPTooLarge) {
		t.Fatalf("mapSNMPError(buffer) = %v, want ErrSNMPTooLarge", got)
	}
}

func TestSNMPClientTimeoutRetriesOnce(t *testing.T) {
	t.Parallel()
	sim := startSNMPSim(t, func([]byte) []byte { return nil })
	c := &gosnmpClient{port: sim.port}
	req := snmpReq(v2cOK, oidSysUpTime)
	req.Timeout = 150 * time.Millisecond
	if _, err := c.Get(context.Background(), req); !errors.Is(err, ErrSNMPTimeout) {
		t.Fatalf("err = %v, want ErrSNMPTimeout", err)
	}
	if n := len(sim.packets()); n != 1+snmpRetries {
		t.Fatalf("paquetes = %d, want %d (intento + 1 reintento)", n, 1+snmpRetries)
	}
}

func TestSNMPClientV3NeverDegradesToV2c(t *testing.T) {
	t.Parallel()
	sim := startSNMPSim(t, func([]byte) []byte { return nil })
	c := &gosnmpClient{port: sim.port}
	req := snmpReq(v3OK, oidSysUpTime)
	req.Timeout = 150 * time.Millisecond
	if _, err := c.Get(context.Background(), req); !errors.Is(err, ErrSNMPTimeout) {
		t.Fatalf("err = %v, want timeout", err)
	}
	pkts := sim.packets()
	if len(pkts) == 0 {
		t.Fatal("no llego ningun paquete")
	}
	for i, p := range pkts {
		if v, ok := versionOf(p); !ok || v != 3 {
			t.Fatalf("paquete %d: version=%d ok=%v, want 3", i, v, ok)
		}
	}
}

// versionOf devuelve el campo version (0=v1, 1=v2c, 3=v3) de un mensaje SNMP.
func versionOf(pkt []byte) (int, bool) {
	_, body, _, ok := readTLV(pkt)
	if !ok {
		return 0, false
	}
	_, ver, _, ok := readTLV(body)
	if !ok || len(ver) != 1 {
		return 0, false
	}
	return int(ver[0]), true
}

func TestSNMPSessionRejectsWeakParams(t *testing.T) {
	t.Parallel()
	c := newGosnmpClient()
	ctx := context.Background()
	for _, name := range []string{"MD5", "SHA1", "SHA", "SHA224", "SHA384", "NONE", ""} {
		p := v3OK
		p.AuthProto = name
		if _, err := c.newSession(ctx, snmpReq(p, oidSysUpTime)); err == nil {
			t.Errorf("auth %q aceptado", name)
		}
	}
	for _, name := range []string{"DES", "AES192", "3DES", "NONE", ""} {
		p := v3OK
		p.PrivProto = name
		if _, err := c.newSession(ctx, snmpReq(p, oidSysUpTime)); err == nil {
			t.Errorf("priv %q aceptado", name)
		}
	}
	p := v3OK
	p.Level = "authNoPriv"
	if _, err := c.newSession(ctx, snmpReq(p, oidSysUpTime)); err == nil {
		t.Error("authNoPriv aceptado")
	}
	for _, v := range []int{0, 1, 4} {
		if _, err := c.newSession(ctx, snmpReq(SNMPParams{Version: v, Community: "x"}, oidSysUpTime)); err == nil {
			t.Errorf("version %d aceptada", v)
		}
	}
	for _, tc := range []struct{ auth, priv string }{{"SHA256", "AES128"}, {"SHA512", "AES256"}} {
		p := v3OK
		p.AuthProto, p.PrivProto = tc.auth, tc.priv
		g, err := c.newSession(ctx, snmpReq(p, oidSysUpTime))
		if err != nil {
			t.Fatalf("%v: %v", tc, err)
		}
		if g.Version != gosnmp.Version3 || g.MsgFlags != gosnmp.AuthPriv || g.Port != snmpPort {
			t.Errorf("%v: version=%v flags=%v port=%d", tc, g.Version, g.MsgFlags, g.Port)
		}
	}
}

func TestMapSNMPError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		want error
	}{
		{fmt.Errorf("x: %w", gosnmp.ErrWrongDigest), ErrSNMPAuth},
		{gosnmp.ErrUnknownUsername, ErrSNMPAuth},
		{gosnmp.ErrDecryption, ErrSNMPAuth},
		{errors.New("request timeout (after 1 retries)"), ErrSNMPTimeout},
		{errors.New("read udp: connection refused"), ErrSNMPTimeout},
	}
	for _, tc := range tests {
		if got := mapSNMPError(context.Background(), tc.err); !errors.Is(got, tc.want) {
			t.Errorf("mapSNMPError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

// fakeSNMP es un SNMPGetter inyectable.
type fakeSNMP struct {
	fn    func(req snmpRequest) (int64, error)
	delay time.Duration

	mu      sync.Mutex
	reqs    []snmpRequest
	cur     int
	maxSeen int
}

func (f *fakeSNMP) Get(ctx context.Context, req snmpRequest) (int64, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.cur++
	f.maxSeen = max(f.maxSeen, f.cur)
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.cur--
		f.mu.Unlock()
	}()
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return 0, ErrSNMPTimeout
		}
	}
	return f.fn(req)
}

func (f *fakeSNMP) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func (f *fakeSNMP) max() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxSeen
}

func snmpCheck(metric string, ifIndex int) Check {
	return Check{ID: "c-" + metric, Type: TypeSNMP, Name: "snmp", Target: "192.0.2.10", TimeoutMS: 3000, Retries: 3,
		Metric: metric, IfIndex: ifIndex, SNMP: v3OK}
}

func newSNMPRunner(t *testing.T, g SNMPGetter, now func() time.Time) *Runner {
	t.Helper()
	r := NewRunner(Options{ProbeAllow: mustProbePolicy(t, "192.0.2.0/24"), SNMP: g, Now: now})
	t.Cleanup(r.Close)
	return r
}

func fixed(v int64) *fakeSNMP {
	return &fakeSNMP{fn: func(snmpRequest) (int64, error) { return v, nil }}
}

func failing(err error) *fakeSNMP {
	return &fakeSNMP{fn: func(snmpRequest) (int64, error) { return 0, err }}
}

func ptr(f float64) *float64 { return &f }

func TestSNMPProbeOutcomes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		c          Check
		g          *fakeSNMP
		wantStatus string
		wantDetail string
		wantValue  *float64
		wantOID    string
	}{
		{"reachable", snmpCheck("reachable", 0), fixed(12345), StatusUp, DetailOK, ptr(123.45), oidSysUpTime},
		{"if_oper up", snmpCheck("if_oper", 3), fixed(1), StatusUp, DetailOK, ptr(1), oidIfOperStatusP + "3"},
		{"if_oper down", snmpCheck("if_oper", 3), fixed(2), StatusDown, DetailOK, ptr(2), ""},
		{"if_oper dormant", snmpCheck("if_oper", 3), fixed(5), StatusWarn, DetailOK, ptr(5), ""},
		{"timeout", snmpCheck("reachable", 0), failing(ErrSNMPTimeout), StatusDown, DetailTimeout, nil, ""},
		{"auth", snmpCheck("reachable", 0), failing(ErrSNMPAuth), StatusUnknown, DetailAuthFailed, nil, ""},
		{"no such", snmpCheck("if_oper", 9), failing(ErrSNMPNoSuchObject), StatusUnknown, DetailNoSuchObject, nil, ""},
		{"giant", snmpCheck("reachable", 0), failing(ErrSNMPTooLarge), StatusUnknown, DetailUnsupported, nil, ""},
		{"otro error", snmpCheck("reachable", 0), failing(errors.New("boom 192.0.2.10 s3cr3t")), StatusUnknown, DetailUnsupported, nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newSNMPRunner(t, tc.g, nil)
			out := r.attempts(context.Background(), tc.c)
			if out.status != tc.wantStatus || out.detail != tc.wantDetail {
				t.Fatalf("got %s/%s, want %s/%s", out.status, out.detail, tc.wantStatus, tc.wantDetail)
			}
			if (out.value == nil) != (tc.wantValue == nil) || (out.value != nil && *out.value != *tc.wantValue) {
				t.Fatalf("value = %v, want %v", out.value, tc.wantValue)
			}
			if n := tc.g.calls(); n != 1 {
				t.Fatalf("llamadas = %d, want 1 (el reintento es del wrapper, no del runner)", n)
			}
			if tc.wantOID != "" && tc.g.reqs[0].OID != tc.wantOID {
				t.Fatalf("oid = %q, want %q", tc.g.reqs[0].OID, tc.wantOID)
			}
			if tc.g.reqs[0].Timeout > snmpMaxTimeout {
				t.Fatalf("timeout = %v, tope %v", tc.g.reqs[0].Timeout, snmpMaxTimeout)
			}
		})
	}
}

func TestSNMPProbeRejectsBeforeSending(t *testing.T) {
	t.Parallel()
	with := func(mut func(*SNMPParams)) SNMPParams { p := v3OK; mut(&p); return p }
	tests := []struct {
		name       string
		mut        func(*Check)
		wantDetail string
	}{
		{"fuera de allowlist", func(c *Check) { c.Target = "198.51.100.5" }, DetailBlocked},
		{"loopback", func(c *Check) { c.Target = "127.0.0.1" }, DetailBlocked},
		{"hostname", func(c *Check) { c.Target = "router.local" }, DetailUnsupported},
		{"if_index 0", func(c *Check) { c.Metric, c.IfIndex = "if_oper", 0 }, DetailUnsupported},
		{"if_index 70000", func(c *Check) { c.Metric, c.IfIndex = "if_oper", 70000 }, DetailUnsupported},
		{"if_index negativo", func(c *Check) { c.Metric, c.IfIndex = "if_oper", -1 }, DetailUnsupported},
		{"reachable con if_index", func(c *Check) { c.IfIndex = 5 }, DetailUnsupported},
		{"metrica libre", func(c *Check) { c.Metric = "1.3.6.1.4.1.2021" }, DetailUnsupported},
		{"MD5", func(c *Check) { c.SNMP = with(func(p *SNMPParams) { p.AuthProto = "MD5" }) }, DetailUnsupported},
		{"SHA1", func(c *Check) { c.SNMP = with(func(p *SNMPParams) { p.AuthProto = "SHA1" }) }, DetailUnsupported},
		{"DES", func(c *Check) { c.SNMP = with(func(p *SNMPParams) { p.PrivProto = "DES" }) }, DetailUnsupported},
		{"authNoPriv", func(c *Check) { c.SNMP = with(func(p *SNMPParams) { p.Level = "authNoPriv" }) }, DetailUnsupported},
		{"sin clave priv", func(c *Check) { c.SNMP = with(func(p *SNMPParams) { p.PrivPass = "" }) }, DetailUnsupported},
		{"v1", func(c *Check) { c.SNMP = SNMPParams{Version: 1, Community: "public"} }, DetailUnsupported},
		{"v2c sin comunidad", func(c *Check) { c.SNMP = SNMPParams{Version: 2} }, DetailUnsupported},
		{"v3 sin usuario", func(c *Check) { c.SNMP = with(func(p *SNMPParams) { p.User = "" }) }, DetailUnsupported},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := fixed(1)
			r := newSNMPRunner(t, g, nil)
			c := snmpCheck("reachable", 0)
			tc.mut(&c)
			out := r.probe(context.Background(), c)
			if out.status != StatusUnknown || out.detail != tc.wantDetail {
				t.Fatalf("got %s/%s, want unknown/%s", out.status, out.detail, tc.wantDetail)
			}
			if g.calls() != 0 {
				t.Fatalf("se enviaron %d solicitudes", g.calls())
			}
		})
	}
}

func TestSNMPEmptyAllowlistBlocks(t *testing.T) {
	t.Parallel()
	g := fixed(1)
	r := NewRunner(Options{SNMP: g})
	if out := r.probe(context.Background(), snmpCheck("reachable", 0)); out.detail != DetailBlocked || g.calls() != 0 {
		t.Fatalf("got %+v calls=%d, want blocked sin trafico", out, g.calls())
	}
}

func TestSNMPBreakerOpensBacksOffAndCloses(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	healthy := false
	g := &fakeSNMP{fn: func(snmpRequest) (int64, error) {
		mu.Lock()
		defer mu.Unlock()
		if healthy {
			return 1, nil
		}
		return 0, ErrSNMPTimeout
	}}
	r := newSNMPRunner(t, g, clock)
	c := snmpCheck("reachable", 0)
	run := func() outcome { return r.probe(context.Background(), c) }

	for range snmpBreakerFails {
		run()
	}
	if g.calls() != snmpBreakerFails {
		t.Fatalf("llamadas = %d", g.calls())
	}
	if out := run(); out.status != StatusDown || g.calls() != snmpBreakerFails {
		t.Fatalf("breaker abierto debe responder down sin trafico: %+v calls=%d", out, g.calls())
	}
	advance(snmpBreakerBase + time.Second)
	run() // 4o fallo: la espera pasa a 60 s
	if g.calls() != snmpBreakerFails+1 {
		t.Fatalf("tras la espera debe reintentar: calls=%d", g.calls())
	}
	advance(snmpBreakerBase + time.Second)
	run()
	if g.calls() != snmpBreakerFails+1 {
		t.Fatalf("la espera crecio a 60 s: calls=%d", g.calls())
	}
	for range 20 { // el backoff nunca pasa del tope
		advance(snmpBreakerMax + time.Second)
		run()
	}
	d := r.snmpDevice(netip.MustParseAddr("192.0.2.10"))
	r.snmpMu.Lock()
	wait := d.openUntil.Sub(clock())
	r.snmpMu.Unlock()
	if wait > snmpBreakerMax || wait <= 0 {
		t.Fatalf("espera = %v, want (0, %v]", wait, snmpBreakerMax)
	}
	mu.Lock()
	healthy = true
	mu.Unlock()
	advance(snmpBreakerMax + time.Second)
	if out := run(); out.status != StatusUp {
		t.Fatalf("got %+v, want up", out)
	}
	r.snmpMu.Lock()
	defer r.snmpMu.Unlock()
	if !d.openUntil.IsZero() || d.fails != 0 {
		t.Fatal("un exito debe cerrar el breaker")
	}
}

func TestSNMPOneRequestPerDevice(t *testing.T) {
	t.Parallel()
	g := fixed(1)
	g.delay = 40 * time.Millisecond
	r := newSNMPRunner(t, g, nil)
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.probe(context.Background(), snmpCheck("reachable", 0))
		}()
	}
	wg.Wait()
	if got := g.max(); got != 1 {
		t.Fatalf("maximo en vuelo por dispositivo = %d, want 1", got)
	}
}

// drainN espera hasta n resultados (o 5 s).
func drainN(r *Runner, n int) []Result {
	var res []Result
	deadline := time.Now().Add(5 * time.Second)
	for len(res) < n && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		res = append(res, r.Drain()...)
	}
	return res
}

func TestSNMPConcurrencyAcrossDevicesIsBounded(t *testing.T) {
	t.Parallel()
	g := fixed(1)
	g.delay = 60 * time.Millisecond
	r := NewRunner(Options{ProbeAllow: mustProbePolicy(t, "192.0.2.0/24"), SNMP: g, MaxChecks: 100})
	set := Set{Rev: "1"}
	for i := 1; i <= 40; i++ {
		c := snmpCheck("reachable", 0)
		c.ID, c.Target, c.IntervalS = fmt.Sprintf("id-%d", i), fmt.Sprintf("192.0.2.%d", i), 3600
		set.Items = append(set.Items, c)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.Apply(ctx, set)
	res := drainN(r, 40)
	cancel()
	r.Close()
	if len(res) != 40 {
		t.Fatalf("resultados = %d, want 40", len(res))
	}
	if got := g.max(); got > MaxSNMPConcurrent || got < 2 {
		t.Fatalf("maximo en vuelo = %d, want 2..%d", got, MaxSNMPConcurrent)
	}
}

func TestSNMPSecretsNeverLeak(t *testing.T) {
	t.Parallel()
	secrets := []string{v3OK.AuthPass, v3OK.PrivPass, v2cOK.Community, "s3cr3t"}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	g := failing(errors.New("boom 192.0.2.10 s3cr3t"))
	r := NewRunner(Options{ProbeAllow: mustProbePolicy(t, "192.0.2.0/24"), SNMP: g, Logger: logger, MaxChecks: 1})
	c := snmpCheck("reachable", 0)
	c.SNMP.Community = v2cOK.Community
	c2 := snmpCheck("if_oper", 1)
	c2.ID = "otro"
	ctx, cancel := context.WithCancel(context.Background())
	r.Apply(ctx, Set{Rev: "1", Items: []Check{c, c2}}) // el 2o excede MaxChecks: se loguea
	res := drainN(r, 2)
	cancel()
	r.Close()
	logger.Info("check", slog.Any("check", c), slog.Any("params", c.SNMP))
	body, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	all := buf.String() + string(body) + fmt.Sprintf("%v %+v %#v %v", c, c, c.SNMP, c.SNMP)
	for _, s := range secrets {
		if strings.Contains(all, s) {
			t.Errorf("secreto %q aparece en logs/resultados/formato", s)
		}
	}
	if strings.Contains(string(body), "192.0.2.10") {
		t.Error("resultado con IP")
	}
}
