package store

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Tests de las alertas de parches (pending_updates, reboot_pending, os_eol), de la lista de flota y de la
// migracion 00028 contra Postgres real.

var updRevSeq int

// upd guarda una foto de parches del host (rev unico por llamada; nil = desconocido).
func upd(t *testing.T, s *Store, host string, total, sec *int, reboot *bool) {
	t.Helper()
	updRevSeq++
	in := UpdatesSnapshot{Rev: fmt.Sprintf("%064x", updRevSeq), PendingTotal: total, PendingSecurity: sec, RebootRequired: reboot,
		Source: "apt", Advisories: []string{}, CollectedAt: time.Now()}
	if err := s.IngestUpdates(context.Background(), hostID(t, host), in, time.Now()); err != nil {
		t.Fatalf("ingest de parches de %s: %v", host, err)
	}
}

func setOS(t *testing.T, host, osName, version string) {
	t.Helper()
	mustExec(t, `UPDATE hosts SET os = 'linux' WHERE hostname = $1`, host)
	mustExec(t, `INSERT INTO host_info (host_id, os_name, os_version) VALUES ($1, $2, $3)
	             ON CONFLICT (host_id) DO UPDATE SET os_name = $2, os_version = $3`, hostID(t, host), osName, version)
}

func updatesRule(name, kind string, threshold float64) AlertRule {
	return AlertRule{Name: name, Kind: kind, Threshold: fptr(threshold), Scope: AlertScope{Type: ScopeAll}, Severity: "warn", Enabled: true}
}

func alertState(t *testing.T) string {
	t.Helper()
	return oneAlert(t).State
}

func TestPendingUpdatesAlertLifecycle(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	rule := updatesRule("seguridad", "pending_updates", 3)
	rule.DurationDays = 2
	mkRule(t, s, rule)

	// Sin datos de parches: no hay candidato.
	if r := evalOnce(t, s); r.Opened != 0 {
		t.Fatalf("sin datos opened got %d, want 0", r.Opened)
	}
	// Cumple el umbral pero lleva menos de 2 dias: no abre.
	upd(t, s, "srv", iptr(12), iptr(5), bptr(false))
	if r := evalOnce(t, s); r.Opened != 0 {
		t.Fatalf("antes de duration_days opened got %d, want 0", r.Opened)
	}
	mustExec(t, `UPDATE updates_current SET security_pending_since = now() - interval '3 days'`)
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("opened got %d, want 1", r.Opened)
	}
	if a := oneAlert(t); a.Trigger == nil || *a.Trigger != 5 || a.Severity != "warn" {
		t.Errorf("alerta got %+v, want trigger 5 y severity warn", a)
	}
	if got := alertDetail(t); got != "5 de seguridad, 12 en total" {
		t.Errorf("detail got %q", got)
	}
	// Dato desconocido: ni resuelve ni abre otra (el detalle anterior se conserva).
	upd(t, s, "srv", nil, nil, nil)
	evalOnce(t, s)
	evalOnce(t, s)
	if st := alertState(t); st != "open" {
		t.Fatalf("desconocido no debe resolver, estado %q", st)
	}
	// Por debajo del umbral (dato conocido): se resuelve.
	upd(t, s, "srv", iptr(2), iptr(1), bptr(false))
	evalOnce(t, s)
	evalOnce(t, s)
	if a := oneAlert(t); a.State != "resolved" || a.Reason != "recovered" {
		t.Errorf("alerta got %+v, want resolved/recovered", a)
	}
}

func TestPendingUpdatesUnknownNeverOpens(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	seedHost(t, "sec0", "up", ago(time.Second))
	mkRule(t, s, updatesRule("seguridad", "pending_updates", 1))
	upd(t, s, "srv", iptr(9), nil, nil) // seguridad desconocida
	upd(t, s, "sec0", iptr(9), iptr(0), bptr(false))
	if r := evalOnce(t, s); r.Opened != 0 {
		t.Fatalf("desconocido o cero no debe abrir, opened got %d", r.Opened)
	}
}

func TestRebootPendingAlertLifecycle(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "srv", "up", ago(time.Second))
	mkRule(t, s, updatesRule("reinicio", "reboot_pending", 2))

	upd(t, s, "srv", iptr(0), iptr(0), bptr(true)) // reinicio pendiente desde ahora
	if r := evalOnce(t, s); r.Opened != 0 {
		t.Fatalf("reinicio reciente opened got %d, want 0", r.Opened)
	}
	mustExec(t, `UPDATE updates_current SET reboot_since = '2026-01-10 12:00+00'`)
	if r := evalOnce(t, s); r.Opened != 1 {
		t.Fatalf("opened got %d, want 1", r.Opened)
	}
	if a := oneAlert(t); a.Trigger == nil || *a.Trigger < 100 {
		t.Errorf("trigger got %v, want >= 100 dias", a.Trigger)
	}
	if got := alertDetail(t); got != "reinicio pendiente desde 2026-01-10" {
		t.Errorf("detail got %q", got)
	}
	// Reinicio desconocido: no resuelve.
	upd(t, s, "srv", iptr(0), iptr(0), nil)
	evalOnce(t, s)
	evalOnce(t, s)
	if st := alertState(t); st != "open" {
		t.Fatalf("reinicio desconocido no debe resolver, estado %q", st)
	}
	// Reinicio hecho (false conocido): se resuelve.
	upd(t, s, "srv", iptr(0), iptr(0), bptr(false))
	evalOnce(t, s)
	evalOnce(t, s)
	if a := oneAlert(t); a.State != "resolved" || a.Reason != "recovered" {
		t.Errorf("alerta got %+v, want resolved/recovered", a)
	}
}

// setEOL fija la fecha de fin de vida de una version y la restaura al terminar (os_eol es la semilla compartida).
func setEOL(t *testing.T, family, cycle, expr string) {
	t.Helper()
	var orig time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT eol_date FROM os_eol WHERE family = $1 AND cycle = $2`, family, cycle).Scan(&orig); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		mustExec(t, `UPDATE os_eol SET eol_date = $3 WHERE family = $1 AND cycle = $2`, family, cycle, orig)
	})
	mustExec(t, `UPDATE os_eol SET eol_date = `+expr+` WHERE family = $1 AND cycle = $2`, family, cycle)
}

func TestOSEOLAlertSoonExpiredAndUnknown(t *testing.T) {
	s := newTestStore(t)
	seedHost(t, "cerca", "up", ago(time.Second))
	seedHost(t, "lejos", "up", ago(time.Second))
	seedHost(t, "vencido", "up", ago(time.Second))
	seedHost(t, "raro", "up", ago(time.Second))
	setOS(t, "cerca", "debian", "12.5")
	setOS(t, "lejos", "ubuntu", "24.04")
	setOS(t, "vencido", "debian", "11.9") // 2026-08-31: ya vencido
	setOS(t, "raro", "arch", "rolling")   // sin fila en os_eol: desconocido
	setEOL(t, "debian", "12", `(now() AT TIME ZONE 'UTC')::date + 20`)
	// La fila 12 de Debian ya es oficial (verificada en 00031): el test necesita una estimada, la marca aqui.
	var origSource string
	if err := testPool.QueryRow(context.Background(), `UPDATE os_eol SET source = 'estimate' WHERE family = 'debian' AND cycle = '12' RETURNING 'official'`).Scan(&origSource); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mustExec(t, `UPDATE os_eol SET source = 'official' WHERE family = 'debian' AND cycle = '12'`) })
	mkRule(t, s, updatesRule("eol", "os_eol", 30))

	if r := evalOnce(t, s); r.Opened != 2 {
		t.Fatalf("opened got %d, want 2 (cerca y vencido)", r.Opened)
	}
	rows, err := testPool.Query(context.Background(),
		`SELECT h.hostname, a.severity, a.trigger_value, a.detail FROM alerts a JOIN hosts h ON h.id = a.host_id ORDER BY h.hostname`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type row struct {
		host, sev string
		val       float64
		detail    string
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.host, &r.sev, &r.val, &r.detail); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].host != "cerca" || got[1].host != "vencido" {
		t.Fatalf("alertas got %+v", got)
	}
	// Cerca: severidad de la regla (warn), 20 dias, version estimada marcada. Vencido: critica aunque la regla sea warn.
	if got[0].sev != "warn" || got[0].val < 19 || got[0].val > 21 || !strings.HasPrefix(got[0].detail, "Debian 12") {
		t.Errorf("cerca got %+v", got[0])
	}
	if want := "estimada"; !strings.Contains(got[0].detail, want) {
		t.Errorf("cerca detail %q debe marcar la fecha como %s", got[0].detail, want)
	}
	if got[1].sev != "critical" || got[1].val >= 0 || !strings.Contains(got[1].detail, "2026-08-31") || strings.Contains(got[1].detail, "estimada") {
		t.Errorf("vencido got %+v", got[1])
	}
	// El SO desconocido no abre ni resuelve nada; con mas antelacion tampoco.
	if n := count(t, `SELECT count(*) FROM alerts a JOIN hosts h ON h.id = a.host_id WHERE h.hostname IN ('raro', 'lejos')`); n != 0 {
		t.Errorf("SO desconocido o lejano no debe alertar, hay %d", n)
	}
	// Se aleja el fin de vida de 'cerca' (>30 dias): se resuelve; 'vencido' sigue.
	mustExec(t, `UPDATE os_eol SET eol_date = (now() AT TIME ZONE 'UTC')::date + 100 WHERE family = 'debian' AND cycle = '12'`)
	evalOnce(t, s)
	evalOnce(t, s)
	if n := count(t, `SELECT count(*) FROM alerts a JOIN hosts h ON h.id = a.host_id WHERE h.hostname = 'cerca' AND a.state = 'resolved'`); n != 1 {
		t.Errorf("cerca debe resolverse, resueltas %d", n)
	}
	if n := count(t, `SELECT count(*) FROM alerts a JOIN hosts h ON h.id = a.host_id WHERE h.hostname = 'vencido' AND a.state = 'open'`); n != 1 {
		t.Errorf("vencido debe seguir abierta, abiertas %d", n)
	}
}

func TestUpdatesAlertsMaintenanceSuppressesAndRespectsGroupScope(t *testing.T) {
	s := newTestStore(t)
	g := mkGroup(t, s, "prod", false, nil)
	for _, n := range []string{"a", "b"} {
		seedHost(t, n, "up", ago(time.Second))
		upd(t, s, n, iptr(4), iptr(4), bptr(true))
	}
	mustExec(t, `UPDATE updates_current SET security_pending_since = now() - interval '5 days', reboot_since = now() - interval '5 days'`)
	setGroupOf(t, "a", g.ID)
	rule := updatesRule("sec", "pending_updates", 1)
	rule.Scope = AlertScope{Type: ScopeGroup, GroupID: &g.ID}
	r1 := mkRule(t, s, rule)
	r2 := mkRule(t, s, updatesRule("reinicio", "reboot_pending", 1))
	mkChannel(t, "hook", "info", r1.ID)
	mkChannel(t, "hook2", "info", r2.ID)
	mustExec(t, `INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type)
		VALUES ('parche', now() - interval '1 hour', now() + interval '1 hour', 'all')`)
	// pending_updates solo el grupo (1 host), reboot_pending a todos (2 hosts).
	if r := evalOnce(t, s); r.Opened != 3 {
		t.Fatalf("opened got %d, want 3", r.Opened)
	}
	if n := count(t, `SELECT count(*) FROM alerts WHERE NOT suppressed`); n != 0 {
		t.Errorf("en mantenimiento todas deben quedar suppressed, hay %d sin suprimir", n)
	}
	if deliveries(t, "opened", "pending") != 0 || deliveries(t, "opened", "suppressed") == 0 {
		t.Error("las entregas opened deben quedar suppressed")
	}
	if n := count(t, `SELECT count(*) FROM alerts a JOIN hosts h ON h.id = a.host_id WHERE a.rule_kind = 'pending_updates' AND h.hostname = 'b'`); n != 0 {
		t.Error("el host fuera del grupo no debe recibir pending_updates")
	}
}

func TestUpdatesRuleRoundTripAndConstraints(t *testing.T) {
	s := newTestStore(t)
	rule := updatesRule("sec", "pending_updates", 5)
	rule.DurationDays = 7
	got := mkRule(t, s, rule)
	if got.DurationDays != 7 || got.Threshold == nil || *got.Threshold != 5 {
		t.Fatalf("regla got %+v", got)
	}
	upd2, err := s.UpdateAlertRule(context.Background(), got.ID, func(r *AlertRule) error { r.DurationDays = 0; return nil })
	if err != nil || upd2.DurationDays != 0 {
		t.Fatalf("update got %+v, %v", upd2.DurationDays, err)
	}
	other := mkRule(t, s, updatesRule("eol", "os_eol", 0))
	for name, sql := range map[string]string{
		"duration_days en otro kind": `UPDATE alert_rules SET duration_days = 1 WHERE id = $1`,
		"umbral eol negativo":        `UPDATE alert_rules SET threshold = -1 WHERE id = $1`,
		"umbral eol enorme":          `UPDATE alert_rules SET threshold = 731 WHERE id = $1`,
		"metric en eol":              `UPDATE alert_rules SET metric = 'cpu_pct' WHERE id = $1`,
		"kind desconocido":           `UPDATE alert_rules SET kind = 'patches' WHERE id = $1`,
	} {
		if _, err := testPool.Exec(context.Background(), sql, other.ID); err == nil {
			t.Errorf("%s: la BD debio rechazarlo", name)
		}
	}
	for name, sql := range map[string]string{
		"pending sin umbral":      `UPDATE alert_rules SET threshold = NULL WHERE id = $1`,
		"pending umbral 0":        `UPDATE alert_rules SET threshold = 0 WHERE id = $1`,
		"pending umbral enorme":   `UPDATE alert_rules SET threshold = 10001 WHERE id = $1`,
		"duration_days fuera":     `UPDATE alert_rules SET duration_days = 366 WHERE id = $1`,
		"pending con clear_thres": `UPDATE alert_rules SET clear_threshold = 1 WHERE id = $1`,
	} {
		if _, err := testPool.Exec(context.Background(), sql, got.ID); err == nil {
			t.Errorf("%s: la BD debio rechazarlo", name)
		}
	}
}

// TestMigrationUpdatesAlertsDown comprueba que Down (00028 -> 27) borra las reglas y alertas de parches sin tocar
// las demas y que Up las vuelve a permitir. Deja la BD en el ultimo esquema.
func TestMigrationUpdatesAlertsDown(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(os.Getenv("ARGOS_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	provider, err := newProvider(pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := provider.Up(ctx); err != nil {
			t.Errorf("restaurando esquema: %v", err)
		}
	})

	seedHost(t, "srv", "up", ago(time.Second))
	mkRule(t, s, updatesRule("reinicio", "reboot_pending", 1))
	mkRule(t, s, AlertRule{Name: "caido", Kind: "host_status", Scope: AlertScope{Type: ScopeAll}, Severity: "critical", Enabled: true})
	upd(t, s, "srv", iptr(1), iptr(0), bptr(true))
	mustExec(t, `UPDATE updates_current SET reboot_since = now() - interval '2 days'`)
	evalOnce(t, s)
	if n := count(t, `SELECT count(*) FROM alerts WHERE rule_kind = 'reboot_pending'`); n != 1 {
		t.Fatalf("alerta reboot_pending got %d, want 1", n)
	}

	if _, err := provider.DownTo(ctx, 27); err != nil {
		t.Fatalf("down a 27: %v", err)
	}
	if n := count(t, `SELECT count(*) FROM alert_rules WHERE kind = 'reboot_pending'`) + count(t, `SELECT count(*) FROM alerts WHERE rule_kind = 'reboot_pending'`); n != 0 {
		t.Errorf("Down debe borrar reglas y alertas de parches, quedan %d", n)
	}
	if n := count(t, `SELECT count(*) FROM alert_rules WHERE kind = 'host_status'`); n != 1 {
		t.Errorf("Down no debe tocar las demas reglas, quedan %d de 1", n)
	}
	if n := count(t, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'alert_rules' AND column_name = 'duration_days'`); n != 0 {
		t.Error("duration_days debe haberse eliminado")
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("up a 28: %v", err)
	}
	_ = count(t, `SELECT count(*) FROM alert_candidates`) // la vista debe existir (count falla si no)
}

// --- flota ---

type fleetSeed struct {
	name       string
	group      string // "" = sin grupo
	total, sec *int
	reboot     *bool
	osName     string
	osVersion  string
}

func seedUpdatesFleet(t *testing.T, s *Store, gA, gB string) {
	t.Helper()
	hosts := []fleetSeed{
		{"h1", "A", iptr(30), iptr(9), bptr(true), "debian", "11.9"},
		{"h2", "A", iptr(12), iptr(3), bptr(false), "debian", "12.5"},
		{"h3", "B", iptr(50), iptr(3), bptr(true), "ubuntu", "24.04"},
		{"h4", "B", iptr(1), nil, nil, "arch", "rolling"}, // seguridad y reinicio desconocidos, SO sin EOL
		{"h5", "", iptr(0), iptr(0), bptr(false), "ubuntu", "22.04"},
	}
	for _, h := range hosts {
		seedHost(t, h.name, "up", ago(time.Second))
		setOS(t, h.name, h.osName, h.osVersion)
		switch h.group {
		case "A":
			setGroupOf(t, h.name, gA)
		case "B":
			setGroupOf(t, h.name, gB)
		}
		upd(t, s, h.name, h.total, h.sec, h.reboot)
	}
	// Sin datos de parches y deshabilitado con datos: no salen en la flota.
	seedHost(t, "sin-datos", "up", ago(time.Second))
	seedHost(t, "off", "disabled", nil)
	upd(t, s, "off", iptr(9), iptr(9), bptr(true))
	// h1 lleva mas tiempo con reinicio pendiente que h3.
	mustExec(t, `UPDATE updates_current SET reboot_since = now() - interval '9 days' WHERE host_id = (SELECT id FROM hosts WHERE hostname = 'h1')`)
	mustExec(t, `UPDATE updates_current SET reboot_since = now() - interval '2 days' WHERE host_id = (SELECT id FROM hosts WHERE hostname = 'h3')`)
}

func fleetNames(p UpdatesFleetPage) []string {
	out := make([]string, len(p.Items))
	for i, r := range p.Items {
		out[i] = r.Hostname
	}
	return out
}

func TestListUpdatesFleetFiltersOrderAndScope(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	gA, gB := mkGroup(t, s, "ga", false, nil), mkGroup(t, s, "gb", false, nil)
	seedUpdatesFleet(t, s, gA.ID, gB.ID)

	list := func(scope Scope, f UpdatesFleetFilter) UpdatesFleetPage {
		t.Helper()
		if f.Sort == "" {
			f.Sort = "security"
			f.Desc = true
		}
		f.Limit = 50
		p, err := s.ListUpdatesFleet(ctx, scope, f)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	check := func(name string, got UpdatesFleetPage, want ...string) {
		t.Helper()
		if !slices.Equal(fleetNames(got), want) {
			t.Errorf("%s got %v, want %v", name, fleetNames(got), want)
		}
		if got.Total != len(want) {
			t.Errorf("%s total got %d, want %d", name, got.Total, len(want))
		}
	}
	// Por seguridad desc, luego nombre; el desconocido (h4) al final. El deshabilitado y sin datos no salen.
	check("seguridad", list(AllScope(), UpdatesFleetFilter{}), "h1", "h2", "h3", "h5", "h4")
	check("seguridad asc", list(AllScope(), UpdatesFleetFilter{Sort: "security"}), "h5", "h2", "h3", "h1", "h4")
	check("pendientes", list(AllScope(), UpdatesFleetFilter{Sort: "pending", Desc: true}), "h3", "h1", "h2", "h4", "h5")
	check("reinicio mas viejo primero", list(AllScope(), UpdatesFleetFilter{Sort: "reboot"}), "h1", "h3", "h2", "h4", "h5")
	check("nombre", list(AllScope(), UpdatesFleetFilter{Sort: "name"}), "h1", "h2", "h3", "h4", "h5")
	check("solo seguridad", list(AllScope(), UpdatesFleetFilter{SecurityOnly: true}), "h1", "h2", "h3")
	check("solo reinicio", list(AllScope(), UpdatesFleetFilter{RebootOnly: true, Sort: "name"}), "h1", "h3")
	check("busqueda", list(AllScope(), UpdatesFleetFilter{Q: "H2"}), "h2")
	check("grupo A", list(AllScope(), UpdatesFleetFilter{GroupIDs: []string{gA.ID}}), "h1", "h2")
	check("sin grupo", list(AllScope(), UpdatesFleetFilter{NoGroup: true}), "h5")
	check("grupo B o sin grupo", list(AllScope(), UpdatesFleetFilter{GroupIDs: []string{gB.ID}, NoGroup: true, Sort: "name"}), "h3", "h4", "h5")
	// EOL: debian 11 ya vencido (2026-08-31); el resto queda lejos o sin dato (h4 no entra).
	zero := 0
	check("eol vencido", list(AllScope(), UpdatesFleetFilter{EOLWithinDays: &zero}), "h1")
	check("eol orden", list(AllScope(), UpdatesFleetFilter{Sort: "eol"}), "h1", "h5", "h2", "h3", "h4") // 2026-08, 2027-04, 2028-06, 2029-05, sin dato
	// Ambito restringido: solo su grupo; los ids ajenos no aparecen ni cuentan en el total.
	check("ambito A", list(GroupScope([]string{gA.ID}), UpdatesFleetFilter{Sort: "name"}), "h1", "h2")
	check("ambito vacio", list(GroupScope(nil), UpdatesFleetFilter{}))
	check("ambito A pide grupo B", list(GroupScope([]string{gA.ID}), UpdatesFleetFilter{GroupIDs: []string{gB.ID}}))
	// El fin de vida sale resuelto en la fila (sin N+1) y desconocido como nil.
	for _, r := range list(AllScope(), UpdatesFleetFilter{Sort: "name"}).Items {
		switch r.Hostname {
		case "h1":
			if r.EOL == nil || r.EOL.Family != "debian" || r.EOL.Cycle != "11" || r.EOL.Estimated {
				t.Errorf("h1 EOL got %+v", r.EOL)
			}
		case "h4":
			if r.EOL != nil || r.Updates.PendingSecurity != nil || r.Updates.RebootRequired != nil {
				t.Errorf("h4 debe tener todo desconocido, got %+v / %+v", r.EOL, r.Updates)
			}
		}
	}
	if _, err := s.ListUpdatesFleet(ctx, AllScope(), UpdatesFleetFilter{Sort: "hack; DROP TABLE hosts", Limit: 5}); err == nil {
		t.Error("un orden fuera de la lista blanca debe rechazarse")
	}
}

func TestListUpdatesFleetCursorWalksEveryHostOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	gA, gB := mkGroup(t, s, "ga", false, nil), mkGroup(t, s, "gb", false, nil)
	seedUpdatesFleet(t, s, gA.ID, gB.ID)
	for _, sortKey := range []string{"security", "pending", "reboot", "eol", "name", "updated"} {
		for _, desc := range []bool{false, true} {
			f := UpdatesFleetFilter{Sort: sortKey, Desc: desc, Limit: 50}
			all, err := s.ListUpdatesFleet(ctx, AllScope(), f)
			if err != nil {
				t.Fatalf("%s: %v", sortKey, err)
			}
			f.Limit = 2
			var walked []string
			for pages := 0; ; pages++ {
				if pages > 10 {
					t.Fatalf("%s desc=%v: el cursor no termina", sortKey, desc)
				}
				p, err := s.ListUpdatesFleet(ctx, AllScope(), f)
				if err != nil {
					t.Fatalf("%s pagina %d: %v", sortKey, pages, err)
				}
				walked = append(walked, fleetNames(p)...)
				if p.NextCursor == nil {
					break
				}
				f.After = p.NextCursor
			}
			if want := fleetNames(all); !slices.Equal(walked, want) {
				t.Errorf("orden %s desc=%v: paginado %v, completo %v", sortKey, desc, walked, want)
			}
		}
	}
}

// TestUpdatesAlertCycleAndFleetPlans1000Hosts mide con 1000 hosts el ciclo de alertas con las tres reglas de parches
// y el listado de flota (una pagina y el total), y deja los tiempos en el log. No hay umbral: los umbrales de
// referencia estan en docs; aqui solo se comprueba que ambos corren y abren lo esperado.
func TestUpdatesAlertCycleAndFleet1000Hosts(t *testing.T) {
	if testing.Short() {
		t.Skip("carga sintetica omitida con -short")
	}
	s := newTestStore(t)
	ctx := context.Background()
	mustExec(t, `INSERT INTO hosts (hostname, os, agent_token_hash, status, last_seen_at)
	             SELECT 'load-' || g, 'linux', 'hash-load-' || g, 'up', now() FROM generate_series(1, 1000) g`)
	mustExec(t, `INSERT INTO host_info (host_id, os_name, os_version)
	             SELECT id, (ARRAY['ubuntu','debian','rhel'])[1 + (row_number() OVER (ORDER BY hostname))::int % 3],
	                        (ARRAY['22.04','12.5','9.3'])[1 + (row_number() OVER (ORDER BY hostname))::int % 3] FROM hosts`)
	mustExec(t, `INSERT INTO updates_current (host_id, rev, pending_total, pending_security, reboot_required, reboot_since,
	                                          security_pending_since, source, collected_at, received_at)
	             SELECT id, lpad(to_hex(row_number() OVER (ORDER BY hostname)), 64, '0'), 20, 1 + (row_number() OVER (ORDER BY hostname))::int % 9, true,
	                    now() - interval '5 days', now() - interval '5 days', 'apt', now(), now() FROM hosts`)
	mkRule(t, s, updatesRule("sec", "pending_updates", 1))
	mkRule(t, s, updatesRule("reboot", "reboot_pending", 1))
	mkRule(t, s, updatesRule("eol", "os_eol", 730))

	start := time.Now()
	first := evalOnce(t, s)
	openDur := time.Since(start)
	start = time.Now()
	steady := evalOnce(t, s)
	steadyDur := time.Since(start)
	t.Logf("ciclo con 1000 hosts x 3 reglas: apertura %v (opened %d), en regimen %v (opened %d)", openDur.Round(time.Millisecond), first.Opened, steadyDur.Round(time.Millisecond), steady.Opened)
	if first.Opened < 2000 || steady.Opened != 0 {
		t.Errorf("opened got %d y %d, want >= 2000 y 0", first.Opened, steady.Opened)
	}
	start = time.Now()
	p, err := s.ListUpdatesFleet(ctx, AllScope(), UpdatesFleetFilter{Sort: "security", Desc: true, Limit: 15})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("flota (1000 hosts, pagina de 15): %v, total %d", time.Since(start).Round(time.Microsecond), p.Total)
	if p.Total != 1000 || len(p.Items) != 15 || p.NextCursor == nil {
		t.Errorf("flota got total %d, items %d", p.Total, len(p.Items))
	}
	for _, q := range []string{
		`SELECT count(*) FROM alert_candidates WHERE breach`,
	} {
		var plan string
		rows, err := testPool.Query(ctx, `EXPLAIN (ANALYZE, TIMING OFF, SUMMARY ON) `+q)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			plan += line + "\n"
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		t.Logf("EXPLAIN %s\n%s", q, plan)
	}
}
