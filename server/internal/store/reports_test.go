package store

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

const (
	hour = 3600
	day  = 24 * hour
)

// at devuelve t0 + s segundos.
func at(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }

// hostEv inserta un evento de host en el instante at(s).
func hostEv(t *testing.T, host, from, to string, s int) {
	t.Helper()
	var f *string
	if from != "" {
		f = &from
	}
	src := "detector"
	if from == "" {
		src = "seed"
	}
	mustExec(t, `INSERT INTO host_events (host_id, from_status, to_status, at, source) VALUES ($1, $2, $3, $4, $5)`,
		hostID(t, host), f, to, at(s), src)
}

func window(t *testing.T, scopeType string, groupID *string, from, to int) {
	t.Helper()
	mustExec(t, `INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type, scope_group_id)
		VALUES ('w', $1, $2, $3, $4)`, at(from), at(to), scopeType, groupID)
}

func approx(got *float64, want float64) bool { return got != nil && math.Abs(*got-want) < 1e-9 }

func byName(hs []HostAvailability) map[string]HostAvailability {
	m := map[string]HostAvailability{}
	for _, h := range hs {
		m[h.Hostname] = h
	}
	return m
}

func TestAvailabilityReportExactSeconds(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	gA, gB := mkGroup(t, s, "A", true, nil), mkGroup(t, s, "B", true, nil)
	for _, h := range []string{"h1", "h2", "h3", "h4", "h5"} {
		seedHost(t, h, "up", ago(time.Second))
		setGroupOf(t, h, gA.ID)
	}
	mustExec(t, `DELETE FROM host_events`)
	from, to := at(0), at(10*day)

	// h1: up, cae 4 h (2 ventanas solapadas + una de otro grupo), offline 6 h, disabled el ultimo dia.
	hostEv(t, "h1", "", "up", -5*day)
	hostEv(t, "h1", "up", "down", 1*day)
	hostEv(t, "h1", "down", "up", 1*day+4*hour)
	hostEv(t, "h1", "up", "offline", 2*day)
	hostEv(t, "h1", "offline", "up", 2*day+6*hour)
	hostEv(t, "h1", "up", "disabled", 9*day)
	window(t, "group", &gA.ID, 1*day+1*hour, 1*day+2*hour+1800)
	window(t, "all", nil, 1*day+2*hour, 1*day+3*hour+1800)
	window(t, "group", &gB.ID, 1*day, 1*day+4*hour) // otro grupo: no aplica
	// h2: pending antes del rango, up desde el dia 5, sin caidas.
	hostEv(t, "h2", "", "pending", -1*day)
	hostEv(t, "h2", "pending", "up", 5*day)
	// h3: host nuevo, primer evento (alta) el dia 6.
	hostEv(t, "h3", "", "up", 6*day)
	// h4: pending todo el rango. h5: disabled todo el rango.
	hostEv(t, "h4", "", "pending", -2*day)
	hostEv(t, "h5", "", "disabled", -2*day)

	got, err := s.AvailabilityReport(ctx, AllScope(), nil, from, to)
	if err != nil {
		t.Fatal(err)
	}
	m := byName(got)
	h1 := m["h1"]
	if h1.UpS != 741600 || h1.DownS != 14400 || h1.DownMaintS != 9000 || h1.OfflineS != 21600 || h1.Incidents != 1 {
		t.Errorf("h1 got %+v", h1)
	}
	if want := 741600.0 / 747000.0 * 100; !approx(h1.Pct(), want) {
		t.Errorf("h1 pct got %v, want %v", h1.Pct(), want)
	}
	if h1.TrackedFrom == nil || !h1.TrackedFrom.Equal(at(-5*day)) || h1.CoveragePct(from, to) != 100 {
		t.Errorf("h1 tracked/coverage got %v / %v", h1.TrackedFrom, h1.CoveragePct(from, to))
	}
	if h2 := m["h2"]; h2.UpS != 5*day || h2.DownS != 0 || !approx(h2.Pct(), 100) || h2.Incidents != 0 {
		t.Errorf("h2 got %+v", h2)
	}
	h3 := m["h3"]
	if h3.UpS != 4*day || h3.CoveragePct(from, to) != 40 || !approx(h3.Pct(), 100) {
		t.Errorf("h3 got %+v cov %v", h3, h3.CoveragePct(from, to))
	}
	for _, n := range []string{"h4", "h5"} {
		if h := m[n]; h.Pct() != nil || h.UpS != 0 || h.DownS != 0 {
			t.Errorf("%s debe ser sin datos (nil), got %+v pct %v", n, h, h.Pct())
		}
	}

	// Grupo: ponderado por tiempo (suma de segundos), no promedio de porcentajes.
	groups := GroupTotals(got, from, to)
	if len(groups) != 1 || groups[0].Hosts != 5 {
		t.Fatalf("grupos got %+v", groups)
	}
	g := groups[0]
	wantUp := int64(741600 + 5*day + 4*day)
	if g.UpS != wantUp || g.DownS != 14400 || g.DownMaintS != 9000 || g.Incidents != 1 {
		t.Errorf("grupo got %+v", g)
	}
	if want := float64(wantUp) / float64(wantUp+5400) * 100; !approx(g.Pct(), want) {
		t.Errorf("grupo pct got %v, want %v", g.Pct(), want)
	}
	fleet := Totals(got, from, to)
	if want := float64(10*day*4+4*day) / float64(5*10*day) * 100; math.Abs(fleet.CoveragePct(from, to)-want) > 1e-9 {
		t.Errorf("cobertura flota got %v, want %v", fleet.CoveragePct(from, to), want)
	}
}

func TestAvailabilityReportScopeAndGroupChange(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	gA, gB := mkGroup(t, s, "A", true, nil), mkGroup(t, s, "B", true, nil)
	for _, h := range []string{"ha", "hb", "hn", "hm"} {
		seedHost(t, h, "up", ago(time.Second))
	}
	setGroupOf(t, "ha", gA.ID)
	setGroupOf(t, "hb", gB.ID)
	setGroupOf(t, "hm", gA.ID) // hm estuvo en A y ahora esta en B: cuenta con todo su historial en B
	mustExec(t, `DELETE FROM host_events`)
	for _, h := range []string{"ha", "hb", "hn", "hm"} {
		hostEv(t, h, "", "up", -1*day)
		hostEv(t, h, "up", "down", 1*day)
		hostEv(t, h, "down", "up", 1*day+hour)
	}
	setGroupOf(t, "hm", gB.ID)
	from, to := at(0), at(2*day)

	names := func(hs []HostAvailability) map[string]bool {
		out := map[string]bool{}
		for _, h := range hs {
			out[h.Hostname] = true
		}
		return out
	}
	for _, tc := range []struct {
		name  string
		scope Scope
		gid   *string
		want  []string
		err   error
	}{
		{"total", AllScope(), nil, []string{"ha", "hb", "hn", "hm"}, nil},
		{"restringido A", GroupScope([]string{gA.ID}), nil, []string{"ha"}, nil},
		{"restringido B (incluye el que cambio)", GroupScope([]string{gB.ID}), nil, []string{"hb", "hm"}, nil},
		{"sin grupos", GroupScope(nil), nil, nil, nil},
		{"valor cero", Scope{}, nil, nil, nil},
		{"group_id propio", GroupScope([]string{gA.ID}), &gA.ID, []string{"ha"}, nil},
		{"group_id ajeno", GroupScope([]string{gA.ID}), &gB.ID, nil, ErrNotFound},
		{"group_id inexistente", GroupScope([]string{gA.ID}), strp("00000000-0000-0000-0000-000000000000"), nil, ErrNotFound},
		{"admin group_id", AllScope(), &gB.ID, []string{"hb", "hm"}, nil},
	} {
		got, err := s.AvailabilityReport(ctx, tc.scope, tc.gid, from, to)
		if !errors.Is(err, tc.err) {
			t.Errorf("%s: err got %v, want %v", tc.name, err, tc.err)
			continue
		}
		n := names(got)
		if len(n) != len(tc.want) {
			t.Errorf("%s: hosts got %v, want %v", tc.name, n, tc.want)
		}
		for _, w := range tc.want {
			if !n[w] {
				t.Errorf("%s: falta %s en %v", tc.name, w, n)
			}
		}
	}
	// hm: historial completo bajo B (1 h caido, 1 incidente).
	got, err := s.AvailabilityReport(ctx, GroupScope([]string{gB.ID}), nil, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if hm := byName(got)["hm"]; hm.DownS != hour || hm.Incidents != 1 || hm.UpS != 2*day-hour {
		t.Errorf("hm got %+v", hm)
	}
	// Sin grupo: el total ve el host sin grupo como un item aparte.
	all, err := s.AvailabilityReport(ctx, AllScope(), nil, from, to)
	if err != nil {
		t.Fatal(err)
	}
	gt := GroupTotals(all, from, to)
	if len(gt) != 3 || gt[2].GroupID != nil || gt[2].Hosts != 1 {
		t.Errorf("GroupTotals got %+v, want A, B y sin grupo al final", gt)
	}
}

func strp(s string) *string { return &s }

func TestReportBounds(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.AvailabilityReport(ctx, AllScope(), nil, at(0), at(0)); !errors.Is(err, ErrReportRange) {
		t.Errorf("rango vacio got %v, want ErrReportRange", err)
	}
	if _, err := s.AvailabilityReport(ctx, AllScope(), nil, at(0), at(0).Add(MaxReportSpan+time.Second)); !errors.Is(err, ErrReportRange) {
		t.Errorf("rango enorme got %v, want ErrReportRange", err)
	}
	mustExec(t, `INSERT INTO hosts (hostname, agent_token_hash, status)
		SELECT 'h' || g, 'hash-' || g, 'up' FROM generate_series(1, $1::int) g`, MaxReportHosts+1)
	if _, err := s.AvailabilityReport(ctx, AllScope(), nil, at(0), at(day)); !errors.Is(err, ErrReportTooLarge) {
		t.Errorf("demasiados hosts got %v, want ErrReportTooLarge", err)
	}
	mustExec(t, `DELETE FROM hosts WHERE hostname = 'h1'`)
	if got, err := s.AvailabilityReport(ctx, AllScope(), nil, at(0), at(day)); err != nil || len(got) != MaxReportHosts {
		t.Errorf("en el tope got %d, %v; want %d, nil", len(got), err, MaxReportHosts)
	}
}

func checkEv(t *testing.T, host, checkID, from, to string, s int) {
	t.Helper()
	var f *string
	if from != "" {
		f = &from
	}
	mustExec(t, `INSERT INTO service_check_events (host_id, check_id, from_status, to_status, at) VALUES ($1, $2, $3, $4, $5)`,
		hostID(t, host), checkID, f, to, at(s))
}

func TestServiceChecksReportExactSeconds(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	gA, gB := mkGroup(t, s, "A", true, nil), mkGroup(t, s, "B", true, nil)
	for _, h := range []string{"h1", "hb"} {
		seedHost(t, h, "up", ago(time.Second))
	}
	setGroupOf(t, "h1", gA.ID)
	setGroupOf(t, "hb", gB.ID)
	mustExec(t, `DELETE FROM host_events`)
	ck := mkCheck(t, s, "web", AlertScope{Type: ScopeAll})
	ckB := mkCheck(t, s, "solo-b", AlertScope{Type: ScopeGroup, GroupID: &gB.ID})
	from, to := at(0), at(10*day)

	// Host h1: up hasta el dia 4, caido 1 dia (dias 4-5), up de nuevo hasta el fin.
	hostEv(t, "h1", "", "up", -3*day)
	hostEv(t, "h1", "up", "down", 4*day)
	hostEv(t, "h1", "down", "up", 5*day)
	// Chequeo: up; warn dias 1-2; down dias 2-3 (host up: cuenta); down otra vez del dia 4 al 6, pero el host
	// estuvo caido del 4 al 5 (congelado: no cuenta) -> solo cuenta el dia 5-6, de los cuales 6 h en mantenimiento.
	checkEv(t, "h1", ck.ID, "", "up", -1*day)
	checkEv(t, "h1", ck.ID, "up", "warn", 1*day)
	checkEv(t, "h1", ck.ID, "warn", "down", 2*day)
	checkEv(t, "h1", ck.ID, "down", "up", 3*day)
	checkEv(t, "h1", ck.ID, "up", "down", 4*day)
	checkEv(t, "h1", ck.ID, "down", "unknown", 6*day)
	window(t, "group", &gA.ID, 5*day+6*hour, 5*day+12*hour)
	checkEv(t, "h1", ck.ID, "unknown", "up", 8*day)
	// Otro grupo (B): el chequeo solo-b existe solo alli.
	hostEv(t, "hb", "", "up", -1*day)
	checkEv(t, "hb", ckB.ID, "", "up", 0)

	got, err := s.ServiceChecksReport(ctx, AllScope(), nil, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("filas got %d (%+v), want 2", len(got), got)
	}
	var c CheckAvailability
	for _, r := range got {
		if r.Hostname == "h1" {
			c = r
		}
	}
	// up: 0-1d (1d) + 3d-4d (1d) + 8d-10d (2d) = 4d; warn 1d; down: 2-3d (1d, host up) + 5-6d (1d) = 2d, 6 h en mant.
	if c.UpS != 4*day || c.WarnS != 1*day || c.DownS != 2*day || c.DownMaintS != 6*hour || c.Incidents != 2 {
		t.Errorf("chequeo got %+v", c)
	}
	if want := float64(5*day) / float64(5*day+2*day-6*hour) * 100; !approx(c.Pct(), want) {
		t.Errorf("pct got %v, want %v", c.Pct(), want)
	}
	if c.TrackedFrom == nil || !c.TrackedFrom.Equal(at(-1*day)) {
		t.Errorf("tracked got %v", c.TrackedFrom)
	}

	// Restringido a A: no ve el host B ni su chequeo; group_id ajeno = 404.
	a, err := s.ServiceChecksReport(ctx, GroupScope([]string{gA.ID}), nil, from, to)
	if err != nil || len(a) != 1 || a[0].Hostname != "h1" {
		t.Errorf("scope A got %+v, %v", a, err)
	}
	if _, err := s.ServiceChecksReport(ctx, GroupScope([]string{gA.ID}), &gB.ID, from, to); !errors.Is(err, ErrNotFound) {
		t.Errorf("group_id ajeno got %v, want ErrNotFound", err)
	}
	if z, err := s.ServiceChecksReport(ctx, Scope{}, nil, from, to); err != nil || len(z) != 0 {
		t.Errorf("valor cero got %+v, %v", z, err)
	}
}
