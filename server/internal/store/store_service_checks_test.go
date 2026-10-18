package store

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"
)

func newCheckID(t *testing.T) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// mkCheck crea un chequeo tcp habilitado con el alcance dado (intervalo 60 s).
func mkCheck(t *testing.T, s *Store, name string, scope AlertScope) ServiceCheckDef {
	t.Helper()
	d, err := s.CreateServiceCheck(context.Background(), ServiceCheckDef{
		ID: newCheckID(t), Name: name, Type: CheckTCP, IntervalSeconds: 60, TimeoutMs: 5000, Retries: 1,
		TargetHost: sptr("localhost"), TargetPort: iptr(22), Scope: scope, Enabled: true,
	}, "")
	if err != nil {
		t.Fatalf("creando chequeo %q: %v", name, err)
	}
	return d
}

func checkIDs(defs []ServiceCheckDef) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.Name)
	}
	sort.Strings(out)
	return out
}

func TestHostChecksResolvesScope(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	g1, g2 := mkGroup(t, s, "g1", false, nil), mkGroup(t, s, "g2", false, nil)
	seedHost(t, "in-g1", "up", ago(time.Second))
	seedHost(t, "in-g2", "up", ago(time.Second))
	seedHost(t, "bare", "up", ago(time.Second))
	if _, err := s.SetHostGroup(ctx, hostID(t, "in-g1"), &g1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetHostGroup(ctx, hostID(t, "in-g2"), &g2.ID); err != nil {
		t.Fatal(err)
	}

	mkCheck(t, s, "all", AlertScope{Type: ScopeAll})
	mkCheck(t, s, "grp1", AlertScope{Type: ScopeGroup, GroupID: &g1.ID})
	off := mkCheck(t, s, "off", AlertScope{Type: ScopeAll})
	if _, err := s.UpdateServiceCheck(ctx, off.ID, func(d *ServiceCheckDef) error { d.Enabled = false; return nil }); err != nil {
		t.Fatal(err)
	}

	for host, want := range map[string][]string{
		"in-g1": {"all", "grp1"},
		"in-g2": {"all"},
		"bare":  {"all"},
	} {
		got, err := s.HostChecks(ctx, hostID(t, host))
		if err != nil {
			t.Fatal(err)
		}
		if names := checkIDs(got); !reflect.DeepEqual(names, want) {
			t.Errorf("%s: chequeos got %v, want %v", host, names, want)
		}
	}
}

func TestHostChecksCapAndDefinitionsCap(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "h", "up", ago(time.Second))
	mustExec(t, `INSERT INTO service_check_defs (name, type, target_host, target_port)
	              SELECT 'c' || g, 'tcp', 'localhost', 22 FROM generate_series(1, 60) g`)
	got, err := s.HostChecks(ctx, hostID(t, "h"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != MaxChecksPerHost+1 {
		t.Fatalf("HostChecks got %d, want %d (tope + 1 pa detectar el truncado)", len(got), MaxChecksPerHost+1)
	}
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i].ID < got[j].ID }) {
		t.Error("los chequeos deben salir ordenados por id")
	}

	mustExec(t, `INSERT INTO service_check_defs (name, type, target_host, target_port)
	              SELECT 'd' || g, 'tcp', 'localhost', 22 FROM generate_series(1, $1::int) g`, MaxServiceChecks-60)
	_, err = s.CreateServiceCheck(ctx, ServiceCheckDef{ID: newCheckID(t), Name: "extra", Type: CheckTCP, IntervalSeconds: 60,
		TimeoutMs: 5000, TargetHost: sptr("h"), TargetPort: iptr(1), Scope: AlertScope{Type: ScopeAll}, Enabled: true}, "")
	if !errors.Is(err, ErrTooManyChecks) {
		t.Fatalf("err got %v, want ErrTooManyChecks", err)
	}
}

func TestServiceCheckCRUDAndConstraints(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	d := mkCheck(t, s, "Mi Check", AlertScope{Type: ScopeAll})
	if _, err := s.CreateServiceCheck(ctx, ServiceCheckDef{ID: newCheckID(t), Name: "mi check", Type: CheckTCP, IntervalSeconds: 60,
		TimeoutMs: 5000, TargetHost: sptr("h"), TargetPort: iptr(1), Scope: AlertScope{Type: ScopeAll}, Enabled: true}, ""); !errors.Is(err, ErrCheckExists) {
		t.Errorf("nombre repetido (otra capitalizacion) got %v, want ErrCheckExists", err)
	}
	missing := "00000000-0000-4000-8000-000000000000"
	if _, err := s.CreateServiceCheck(ctx, ServiceCheckDef{ID: newCheckID(t), Name: "x", Type: CheckTCP, IntervalSeconds: 60,
		TimeoutMs: 5000, TargetHost: sptr("h"), TargetPort: iptr(1), Scope: AlertScope{Type: ScopeGroup, GroupID: &missing}, Enabled: true}, ""); !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("grupo inexistente got %v, want ErrGroupNotFound", err)
	}
	// El secreto solo existe con su cabecera y en chequeos http.
	err := execErr(`UPDATE service_check_defs SET secret_enc = '\x01', secret_key_id = 'k' WHERE id = $1`, d.ID)
	if err == nil {
		t.Error("un chequeo tcp no debe admitir secreto")
	}
	if _, _, err := s.DeleteServiceCheck(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("borrar inexistente got %v, want ErrNotFound", err)
	}
	if _, err := s.ServiceCheckByID(ctx, AllScope(), missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("leer inexistente got %v, want ErrNotFound", err)
	}
	if name, typ, err := s.DeleteServiceCheck(ctx, d.ID); err != nil || name != "Mi Check" || typ != CheckTCP {
		t.Errorf("borrar got (%q, %q, %v)", name, typ, err)
	}
}

func execErr(sql string, args ...any) error {
	_, err := testPool.Exec(context.Background(), sql, args...)
	return err
}

func TestChannelKeyIDsIncludesCheckSecrets(t *testing.T) {
	s := newTestStore(t)
	mustExec(t, `INSERT INTO service_check_defs (name, type, url, header_name, secret_enc, secret_key_id)
	              VALUES ('h', 'http', 'http://localhost/x', 'Authorization', '\x01', 'key-a')`)
	ids, err := s.ChannelKeyIDs(context.Background())
	if err != nil || len(ids) != 1 || ids[0] != "key-a" {
		t.Fatalf("ChannelKeyIDs got (%v, %v), want [key-a]", ids, err)
	}
}

// --- ingest de resultados ---

func result(id, status string, at time.Time) CheckResult {
	return CheckResult{CheckID: id, Status: status, ResponseMs: 12, Detail: "ok", TS: at}
}

func ingestResults(t *testing.T, s *Store, hostID string, rs ...CheckResult) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	// Cada ingest usa un ts de metricas distinto: el reporte en si no es el objeto de estos tests.
	ts := now.Add(time.Duration(count(t, `SELECT count(*) FROM metrics`)) * time.Second)
	if _, err := s.IngestMetrics(context.Background(), hostID,
		Sample{Timestamp: ts, MemUsedMB: 1, MemTotalMB: 2, CheckResults: rs}); err != nil {
		t.Fatalf("ingest: %v", err)
	}
}

type stateRow struct {
	Status        string
	ChangedAt, TS time.Time
	Detail        *string
}

func stateOf(t *testing.T, host, checkID string) (stateRow, bool) {
	t.Helper()
	var r stateRow
	err := testPool.QueryRow(context.Background(),
		`SELECT status, status_changed_at, ts, detail FROM service_check_state WHERE host_id = $1 AND check_id = $2`,
		host, checkID).Scan(&r.Status, &r.ChangedAt, &r.TS, &r.Detail)
	if err != nil {
		return stateRow{}, false
	}
	return r, true
}

func TestIngestCheckResultsStateEventsAndSampling(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	host := enrolled(t, s, "h1")
	other := enrolled(t, s, "h2")
	g := mkGroup(t, s, "solo-h2", false, nil)
	if _, err := s.SetHostGroup(ctx, other, &g.ID); err != nil {
		t.Fatal(err)
	}
	mine := mkCheck(t, s, "mine", AlertScope{Type: ScopeAll})
	foreign := mkCheck(t, s, "foreign", AlertScope{Type: ScopeGroup, GroupID: &g.ID})
	disabled := mkCheck(t, s, "disabled", AlertScope{Type: ScopeAll})
	if _, err := s.UpdateServiceCheck(ctx, disabled.ID, func(d *ServiceCheckDef) error { d.Enabled = false; return nil }); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Truncate(time.Second).Add(-30 * time.Minute)

	// Primer resultado: crea estado, evento (from NULL) y muestra. Los ajenos y deshabilitados se descartan.
	ingestResults(t, s, host, result(mine.ID, "up", base), result(foreign.ID, "down", base), result(disabled.ID, "up", base),
		result("00000000-0000-4000-8000-000000000000", "up", base))
	if n := count(t, `SELECT count(*) FROM service_check_state`); n != 1 {
		t.Fatalf("estados got %d, want 1 (solo el chequeo asignado)", n)
	}
	if n := count(t, `SELECT count(*) FROM service_check_events WHERE from_status IS NULL AND to_status = 'up'`); n != 1 {
		t.Errorf("evento inicial got %d, want 1", n)
	}
	if n := count(t, `SELECT count(*) FROM service_checks WHERE check_id = $1 AND name IS NULL`, mine.ID); n != 1 {
		t.Errorf("muestras got %d, want 1", n)
	}

	// Sin transicion y antes de 15 min: solo actualiza el estado.
	ingestResults(t, s, host, result(mine.ID, "up", base.Add(time.Minute)))
	if st, _ := stateOf(t, host, mine.ID); !st.TS.Equal(base.Add(time.Minute)) || !st.ChangedAt.Equal(base) {
		t.Errorf("estado got ts=%v changed=%v, want ts=%v changed=%v", st.TS, st.ChangedAt, base.Add(time.Minute), base)
	}
	if n := count(t, `SELECT count(*) FROM service_checks WHERE check_id = $1`, mine.ID); n != 1 {
		t.Errorf("muestras sin transicion ni 15 min got %d, want 1", n)
	}

	// Reenvio del buffer (ts no mas nuevo que el estado): ignorado por completo.
	ingestResults(t, s, host, result(mine.ID, "down", base), result(mine.ID, "down", base.Add(time.Minute)))
	if st, _ := stateOf(t, host, mine.ID); st.Status != "up" {
		t.Errorf("un reenvio no debe pisar el estado, got %s", st.Status)
	}
	if n := count(t, `SELECT count(*) FROM service_check_events`); n != 1 {
		t.Errorf("eventos tras reenvio got %d, want 1", n)
	}

	// Transicion: evento, muestra y status_changed_at = ts del resultado.
	ingestResults(t, s, host, result(mine.ID, "down", base.Add(2*time.Minute)))
	st, _ := stateOf(t, host, mine.ID)
	if st.Status != "down" || !st.ChangedAt.Equal(base.Add(2*time.Minute)) {
		t.Errorf("tras caida got %s changed=%v", st.Status, st.ChangedAt)
	}
	if n := count(t, `SELECT count(*) FROM service_check_events WHERE from_status = 'up' AND to_status = 'down' AND at = $1`, base.Add(2*time.Minute)); n != 1 {
		t.Errorf("evento de transicion got %d, want 1", n)
	}
	if n := count(t, `SELECT count(*) FROM service_checks WHERE check_id = $1`, mine.ID); n != 2 {
		t.Errorf("muestras tras transicion got %d, want 2", n)
	}

	// >= 15 min desde la ultima muestra: muestra sin transicion.
	ingestResults(t, s, host, result(mine.ID, "down", base.Add(18*time.Minute)))
	if n := count(t, `SELECT count(*) FROM service_checks WHERE check_id = $1`, mine.ID); n != 3 {
		t.Errorf("muestras tras 15 min got %d, want 3", n)
	}
	if n := count(t, `SELECT count(*) FROM service_check_events`); n != 2 {
		t.Errorf("eventos got %d, want 2", n)
	}

	// Varios resultados del mismo chequeo en un reporte, desordenados: se procesan por ts.
	ingestResults(t, s, host, result(mine.ID, "up", base.Add(21*time.Minute)), result(mine.ID, "warn", base.Add(20*time.Minute)))
	if st, _ := stateOf(t, host, mine.ID); st.Status != "up" || !st.TS.Equal(base.Add(21*time.Minute)) {
		t.Errorf("estado final got %s ts=%v, want up", st.Status, st.TS)
	}
	if n := count(t, `SELECT count(*) FROM service_check_events WHERE check_id = $1`, mine.ID); n != 4 {
		t.Errorf("eventos got %d, want 4 (up, down, warn, up)", n)
	}
}

func TestIngestLegacyServicesKeepNoStateNorEvents(t *testing.T) {
	s := newTestStore(t)
	host := enrolled(t, s, "h1")
	mkCheck(t, s, "c", AlertScope{Type: ScopeAll})
	if _, err := s.IngestMetrics(context.Background(), host, Sample{
		Timestamp: time.Now().Truncate(time.Second), MemUsedMB: 1, MemTotalMB: 2,
		Services: []ServiceCheck{{"nginx", "up", 5}},
	}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, `SELECT count(*) FROM service_checks WHERE name = 'nginx' AND check_id IS NULL`); n != 1 {
		t.Errorf("muestra legada got %d, want 1", n)
	}
	if n := count(t, `SELECT (SELECT count(*) FROM service_check_state) + (SELECT count(*) FROM service_check_events)`); n != 0 {
		t.Errorf("lo legado no crea estado ni eventos, got %d", n)
	}
	if err := execErr(`INSERT INTO service_checks (host_id, status, ts) VALUES ($1, 'up', now())`, host); err == nil {
		t.Error("una muestra sin name ni check_id debe violar el CHECK de identidad")
	}
}

func TestHostServiceChecksAndEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	host := enrolled(t, s, "h1")
	c := mkCheck(t, s, "c", AlertScope{Type: ScopeAll})
	mkCheck(t, s, "pending", AlertScope{Type: ScopeAll})
	base := time.Now().Truncate(time.Second).Add(-10 * time.Minute)
	for i, st := range []string{"up", "down", "up"} {
		ingestResults(t, s, host, result(c.ID, st, base.Add(time.Duration(i)*time.Minute)))
	}
	got, err := s.HostServiceChecks(ctx, AllScope(), host)
	if err != nil || len(got) != 2 {
		t.Fatalf("HostServiceChecks got (%d, %v), want 2", len(got), err)
	}
	if got[0].Def.Name != "c" || got[0].State == nil || got[0].State.Status != "up" || got[1].State != nil {
		t.Errorf("estados got %+v / %+v; el que no reporto debe venir sin estado", got[0].State, got[1].State)
	}
	if _, err := s.HostServiceChecks(ctx, AllScope(), "00000000-0000-4000-8000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("host inexistente got %v, want ErrNotFound", err)
	}

	p1, err := s.ListCheckEvents(ctx, AllScope(), host, c.ID, nil, 2)
	if err != nil || len(p1.Items) != 2 || p1.NextCursor == nil || p1.Items[0].ToStatus != "up" || p1.Items[1].ToStatus != "down" {
		t.Fatalf("pagina 1 got %+v err=%v", p1, err)
	}
	p2, err := s.ListCheckEvents(ctx, AllScope(), host, c.ID, p1.NextCursor, 2)
	if err != nil || len(p2.Items) != 1 || p2.NextCursor != nil || p2.Items[0].FromStatus != nil {
		t.Fatalf("pagina 2 got %+v err=%v", p2, err)
	}
	if _, err := s.ListCheckEvents(ctx, AllScope(), "00000000-0000-4000-8000-000000000000", c.ID, nil, 5); !errors.Is(err, ErrNotFound) {
		t.Errorf("eventos de host inexistente got %v, want ErrNotFound", err)
	}
}

// --- alertas service_check ---

func svcRule(name, checkID string, durationSec int) AlertRule {
	return AlertRule{
		Name: name, Kind: "service_check", CheckID: &checkID, DurationSeconds: durationSec, ResolveSeconds: 0,
		Scope: AlertScope{Type: ScopeAll}, Severity: "critical", Enabled: true,
	}
}

// setState fija el estado de un chequeo en un host (ts = ahora - tsAgo; cambio = ahora - changedAgo).
func setState(t *testing.T, host, checkID, status string, tsAgo, changedAgo time.Duration) {
	t.Helper()
	mustExec(t,
		`INSERT INTO service_check_state (host_id, check_id, status, status_changed_at, ts, response_ms, last_sample_at)
		 VALUES ($1, $2, $3, now() - make_interval(secs => $5::float8), now() - make_interval(secs => $4::float8), 20, now())
		 ON CONFLICT (host_id, check_id) DO UPDATE SET status = EXCLUDED.status, ts = EXCLUDED.ts,
		   status_changed_at = EXCLUDED.status_changed_at`,
		host, checkID, status, tsAgo.Seconds(), changedAgo.Seconds())
}

func TestEvaluateServiceCheckLifecycle(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "h", "up", ago(time.Second))
	h := hostID(t, "h")
	c := mkCheck(t, s, "svc", AlertScope{Type: ScopeAll})
	rule := mkRule(t, s, svcRule("svc caido", c.ID, 300))

	// Caido hace 10 s: incumple pero no cumple duration (300 s): pendiente.
	setState(t, h, c.ID, "down", 10*time.Second, 10*time.Second)
	if r := evalOnce(t, s); r.Opened != 0 {
		t.Fatalf("no debe abrir antes de duration, opened=%d", r.Opened)
	}
	if n := count(t, `SELECT count(*) FROM alert_pending WHERE rule_id = $1`, rule.ID); n != 1 {
		t.Fatalf("pendientes got %d, want 1", n)
	}

	// Recupera antes de duration (anti-flapping): el pendiente se descarta y no se abre nada.
	setState(t, h, c.ID, "up", 5*time.Second, 5*time.Second)
	evalOnce(t, s)
	if n := count(t, `SELECT count(*) FROM alert_pending`) + count(t, `SELECT count(*) FROM alerts`); n != 0 {
		t.Fatalf("un parpadeo no debe alertar, got %d", n)
	}

	// Cae de nuevo desde hace 10 min: abre de inmediato (breach_since = status_changed_at).
	setState(t, h, c.ID, "down", 5*time.Second, 10*time.Minute)
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("opened got %d, want 1", r.Opened)
	}
	a := oneAlert(t)
	if a.State != "open" || a.Severity != "critical" || a.Trigger == nil || *a.Trigger != 20 {
		t.Fatalf("alerta got %+v, want open critical con valor 20", a)
	}
	if n := count(t, `SELECT count(*) FROM alerts WHERE rule_kind = 'service_check' AND rule_id = $1`, rule.ID); n != 1 {
		t.Errorf("la alerta debe registrar rule_kind service_check")
	}

	// Estado viejo (mas de 3 intervalos): ni resuelve aunque el ultimo dato sea up.
	setState(t, h, c.ID, "up", 4*time.Minute, 4*time.Minute)
	evalOnce(t, s)
	if a := oneAlert(t); a.State != "open" {
		t.Fatalf("un estado viejo no resuelve, got %s", a.State)
	}

	// Recupera con dato fresco: se resuelve.
	setState(t, h, c.ID, "up", 5*time.Second, 5*time.Second)
	if r := evalOnce(t, s); r.Resolved != 1 {
		t.Fatalf("resolved got %d, want 1", r.Resolved)
	}
	if a := oneAlert(t); a.State != "resolved" || a.Reason != "recovered" {
		t.Fatalf("alerta got %+v, want resolved/recovered", a)
	}
}

func TestEvaluateServiceCheckStaleStateDoesNotBreach(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "h", "up", ago(time.Second))
	c := mkCheck(t, s, "svc", AlertScope{Type: ScopeAll})
	mkRule(t, s, svcRule("r", c.ID, 0))
	setState(t, hostID(t, "h"), c.ID, "down", 4*time.Minute, 10*time.Minute) // 4 min > 3 x 60 s
	if r := evalOnce(t, s); r.Opened != 0 {
		t.Fatalf("un down viejo no debe abrir, opened=%d", r.Opened)
	}
}

func TestEvaluateServiceCheckWarnOption(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "h", "up", ago(time.Second))
	h := hostID(t, "h")
	c := mkCheck(t, s, "svc", AlertScope{Type: ScopeAll})
	rule := svcRule("r", c.ID, 0)
	created := mkRule(t, s, rule)
	setState(t, h, c.ID, "warn", 5*time.Second, time.Minute)
	if r := evalOnce(t, s); r.Opened != 0 {
		t.Fatalf("warn no incumple por defecto, opened=%d", r.Opened)
	}
	if _, err := s.UpdateAlertRule(context.Background(), created.ID, func(r *AlertRule) error { r.WarnOrDown = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("con warn_or_down warn abre, opened=%d", r.Opened)
	}
	// unknown ni incumple ni recupera.
	setState(t, h, c.ID, "unknown", 5*time.Second, time.Second)
	if r := evalOnce(t, s); r.Resolved != 0 {
		t.Fatalf("unknown no resuelve, resolved=%d", r.Resolved)
	}
}

func TestEvaluateServiceCheckOfflineHostAndMaintenance(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "lap", "offline", ago(time.Hour))
	seedHost(t, "srv", "up", ago(time.Second))
	c := mkCheck(t, s, "svc", AlertScope{Type: ScopeAll})
	mkRule(t, s, svcRule("r", c.ID, 0))
	setState(t, hostID(t, "lap"), c.ID, "down", 5*time.Second, time.Minute)
	if r := evalOnce(t, s); r.Opened != 0 {
		t.Fatalf("un host offline no evalua, opened=%d", r.Opened)
	}

	// Alerta abierta que sigue abierta cuando el host pasa a offline.
	setState(t, hostID(t, "srv"), c.ID, "down", 5*time.Second, time.Minute)
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("opened got %d, want 1", r.Opened)
	}
	mustExec(t, `UPDATE hosts SET status = 'offline' WHERE hostname = 'srv'`)
	setState(t, hostID(t, "srv"), c.ID, "up", 5*time.Second, time.Second)
	evalOnce(t, s)
	if a := oneAlert(t); a.State != "open" {
		t.Fatalf("alerta con host offline debe seguir abierta, got %s", a.State)
	}

	// Mantenimiento: la alerta se abre suprimida.
	mustExec(t, `TRUNCATE alerts CASCADE`)
	mustExec(t, `UPDATE hosts SET status = 'up' WHERE hostname = 'srv'`)
	setState(t, hostID(t, "srv"), c.ID, "down", 5*time.Second, time.Minute)
	mustExec(t, `INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type)
	             VALUES ('m', now() - interval '1 hour', now() + interval '1 hour', 'all')`)
	evalOnce(t, s)
	if a := oneAlert(t); !a.Suppressed {
		t.Fatalf("en mantenimiento la alerta nace suprimida, got %+v", a)
	}
}

func TestServiceCheckRuleConstraintsAndCascade(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	c := mkCheck(t, s, "svc", AlertScope{Type: ScopeAll})
	missing := "00000000-0000-4000-8000-000000000000"
	if _, err := s.CreateAlertRule(ctx, svcRule("x", missing, 0), ""); !errors.Is(err, ErrCheckNotFound) {
		t.Errorf("check inexistente got %v, want ErrCheckNotFound", err)
	}
	bad := svcRule("y", c.ID, 0)
	bad.Metric = sptr("cpu_pct")
	if _, err := s.CreateAlertRule(ctx, bad, ""); err == nil {
		t.Error("una regla service_check con metric debe violar el CHECK")
	}
	rule := mkRule(t, s, svcRule("ok", c.ID, 0))
	if rule.CheckName == nil || *rule.CheckName != "svc" {
		t.Errorf("CheckName got %v, want svc", rule.CheckName)
	}
	if _, _, err := s.DeleteServiceCheck(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AlertRuleByID(ctx, AllScope(), rule.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("la regla debe caer con su chequeo, got %v", err)
	}
}

// --- retencion ---

func TestPurgeCheckEventsAndStaleState(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedHost(t, "h", "up", ago(time.Second))
	h := hostID(t, "h")
	c1 := mkCheck(t, s, "c1", AlertScope{Type: ScopeAll})
	c2 := mkCheck(t, s, "c2", AlertScope{Type: ScopeAll})
	c3 := mkCheck(t, s, "c3", AlertScope{Type: ScopeAll})
	mustExec(t, `INSERT INTO service_check_events (host_id, check_id, to_status, at)
	              VALUES ($1, $2, 'up', now() - interval '200 days'), ($1, $2, 'down', now() - interval '10 days')`, h, c1.ID)
	if n, err := s.PurgeCheckEvents(ctx, time.Now().Add(-180*24*time.Hour), 1000); err != nil || n != 1 {
		t.Fatalf("PurgeCheckEvents got (%d, %v), want 1", n, err)
	}

	// Estados sin actualizar > 3 dias se borran, salvo el de una alerta todavia activa.
	setState(t, h, c1.ID, "up", 4*24*time.Hour, 4*24*time.Hour)
	setState(t, h, c2.ID, "up", time.Hour, time.Hour)
	setState(t, h, c3.ID, "down", 5*24*time.Hour, 5*24*time.Hour)
	rule := mkRule(t, s, svcRule("r3", c3.ID, 0))
	mustExec(t, `INSERT INTO alerts (rule_id, host_id, rule_name, rule_kind, severity) VALUES ($1, $2, 'r3', 'service_check', 'critical')`, rule.ID, h)
	if n, err := s.PurgeStaleCheckState(ctx, time.Now(), 1000); err != nil || n != 1 {
		t.Fatalf("PurgeStaleCheckState got (%d, %v), want 1", n, err)
	}
	if n := count(t, `SELECT count(*) FROM service_check_state WHERE check_id = ANY($1::uuid[])`, []string{c2.ID, c3.ID}); n != 2 {
		t.Errorf("deben quedar el estado fresco y el de la alerta activa, got %d", n)
	}
}

func TestMigrateDownToSixteenAndUpAgainWithCheckData(t *testing.T) {
	if testPool == nil {
		t.Skip("ARGOS_TEST_DATABASE_URL no definida")
	}
	ctx := context.Background()
	s := newTestStore(t)
	if err := resetSchema(ctx); err != nil {
		t.Fatal(err)
	}
	seedHost(t, "h", "up", ago(time.Second))
	c := mkCheck(t, s, "svc", AlertScope{Type: ScopeAll})
	mkRule(t, s, svcRule("r", c.ID, 0))
	setState(t, hostID(t, "h"), c.ID, "down", time.Second, time.Second)
	evalOnce(t, s)
	ingestResults(t, s, hostID(t, "h"), result(c.ID, "down", time.Now().Add(-time.Minute)))
	mustExec(t, `INSERT INTO service_checks (host_id, name, status, ts) VALUES ($1, 'legado', 'up', now())`, hostID(t, "h"))

	provider, err := newProvider(testPool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.DownTo(ctx, 16); err != nil {
		t.Fatalf("down a 16: %v", err)
	}
	if n := count(t, `SELECT count(*) FROM pg_tables WHERE schemaname = 'public' AND tablename IN ('service_check_defs', 'service_check_state', 'service_check_events')`); n != 0 {
		t.Errorf("las tablas de chequeos siguen tras down: %d", n)
	}
	if n := count(t, `SELECT count(*) FROM service_checks WHERE name = 'legado'`); n != 1 {
		t.Errorf("la muestra legada debe sobrevivir al down, got %d", n)
	}
	if n := count(t, `SELECT count(*) FROM alerts WHERE rule_kind = 'service_check'`); n != 0 {
		t.Errorf("las alertas service_check deben irse en el down")
	}
	if err := Migrate(ctx, testPool); err != nil {
		t.Fatalf("up tras down: %v", err)
	}
}

func TestFailingHostsByCheckCountsDownAndWarnWithinScope(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "lima", false, nil)
	seedHost(t, "a", "up", ago(time.Second))
	seedHost(t, "b", "up", ago(time.Second))
	seedHost(t, "c", "up", ago(time.Second))
	setGroupOf(t, "a", g.ID)
	c := mkPing(t, s, "ping", AlertScope{Type: ScopeAll})
	setState(t, hostID(t, "a"), c.ID, "down", time.Second, time.Second)
	setState(t, hostID(t, "b"), c.ID, "warn", time.Second, time.Second)
	setState(t, hostID(t, "c"), c.ID, "up", time.Second, time.Second)

	got, err := s.FailingHostsByCheck(ctx, AllScope())
	if err != nil || got[c.ID] != 2 {
		t.Errorf("ambito total got (%v, %v), want %s:2", got, err, c.ID)
	}
	got, err = s.FailingHostsByCheck(ctx, GroupScope([]string{g.ID}))
	if err != nil || got[c.ID] != 1 {
		t.Errorf("ambito del grupo got (%v, %v), want %s:1", got, err, c.ID)
	}
}
