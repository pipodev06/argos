package store

import (
	"context"
	_ "embed"
	"testing"
	"time"
)

// legacyAlertCandidatesSQL es la vista tal como quedo en la migracion 00029 (antes de la 00035), con otro nombre:
// sirve de referencia para el test de equivalencia. Vive en testdata para no duplicar el SQL en el código Go.
//
//go:embed testdata/alert_candidates_legacy.sql
var legacyAlertCandidatesSQL string

// TestAlertCandidatesEquivalentToLegacy siembra un escenario con los diez tipos de candidato (metric, host_status,
// no_data, service_check, exposed_port, pending_updates, os_eol, mantenimiento, hosts deshabilitados, dispositivos)
// y compara fila a fila (severidad implicita en breach/clear, detail y la clave rule_id/host_id) la vista optimizada
// de la 00035 contra la version anterior. Regresion de la reescritura del LATERAL de os_eol/exposed_port a UNION ALL.
func TestAlertCandidatesEquivalentToLegacy(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := testPool.Exec(ctx, legacyAlertCandidatesSQL); err != nil {
		t.Fatalf("creando vista legacy: %v", err)
	}
	defer func() {
		if _, err := testPool.Exec(ctx, `DROP VIEW alert_candidates_legacy`); err != nil {
			t.Errorf("borrando vista legacy: %v", err)
		}
	}()

	// Grupo y hosts: uno normal en el grupo, uno fuera de grupo, un colector para el dispositivo, un deshabilitado
	// y un pending (ninguno de los dos ultimos debe aparecer como candidato).
	g := mkGroup(t, s, "clientes", true, nil)
	seedHost(t, "web1", "up", ago(time.Second))
	mustExec(t, `UPDATE hosts SET group_id = $1 WHERE hostname = 'web1'`, g.ID)
	seedHost(t, "web2", "down", ago(2*time.Hour))
	seedHost(t, "collector", "up", ago(time.Second))
	seedHost(t, "off", "disabled", nil)
	mustExec(t, `UPDATE hosts SET agent_token_revoked_at = now() WHERE hostname = 'off'`)
	seedHost(t, "pend", "pending", nil)

	dev := mkDevice(t, s, "impresora", "10.9.0.5", "collector", nil)

	// metric: web1 con cpu alto.
	setLatest(t, "web1", 95)
	mkRule(t, s, metricRule("cpu alto", "cpu_pct", "gt", 90, 0))

	// host_status: web2 esta down; regla a todos.
	mkRule(t, s, AlertRule{Name: "caido", Kind: "host_status", DurationSeconds: 0, ResolveSeconds: 0,
		Scope: AlertScope{Type: ScopeAll}, Severity: "critical", Enabled: true})

	// host_status sobre el dispositivo (scope device).
	mkRule(t, s, AlertRule{Name: "impresora caida", Kind: "host_status", DurationSeconds: 0, ResolveSeconds: 0,
		Scope: AlertScope{Type: ScopeDevice, HostID: &dev.ID}, Severity: "critical", Enabled: true})
	mustExec(t, `UPDATE hosts SET status = 'down', status_changed_at = now() - interval '1 hour' WHERE id = $1`, dev.ID)

	// no_data: sin muestra reciente en collector (usa last_seen_at, que seedHost ya fijo a hace 1 s: forzamos mas viejo).
	mkRule(t, s, AlertRule{Name: "sin datos", Kind: "no_data", Threshold: fptr(300), DurationSeconds: 0, ResolveSeconds: 0,
		Scope: AlertScope{Type: ScopeAll}, Severity: "warn", Enabled: true})
	mustExec(t, `UPDATE hosts SET last_seen_at = now() - interval '1 hour' WHERE hostname = 'collector'`)

	// service_check: chequeo ping sobre el dispositivo, en down.
	chk := mkPing(t, s, "ping impresora", AlertScope{Type: ScopeDevice, HostID: &dev.ID})
	setState(t, dev.ID, chk.ID, "down", time.Second, 30*time.Second)
	mkRule(t, s, AlertRule{Name: "ping caido", Kind: "service_check", CheckID: &chk.ID, DurationSeconds: 0, ResolveSeconds: 0,
		Scope: AlertScope{Type: ScopeAll}, Severity: "warn", Enabled: true})

	// exposed_port: puerto nuevo expuesto en web1.
	ingestPorts(t, s, "web1")
	ingestPorts(t, s, "web1", portItem("tcp", "0.0.0.0", 4444))
	mkRule(t, s, exposedRule("puertos"))

	// pending_updates: parches de seguridad viejos en web1.
	upd(t, s, "web1", iptr(20), iptr(5), bptr(false))
	mustExec(t, `UPDATE updates_current SET security_pending_since = now() - interval '5 days'`)
	pu := updatesRule("parches", "pending_updates", 3)
	pu.DurationDays = 2
	mkRule(t, s, pu)

	// os_eol: SO vencido en web2.
	setOS(t, "web2", "ubuntu", "18.04")
	eo := updatesRule("eol", "os_eol", 30)
	mkRule(t, s, eo)

	// mantenimiento: ventana activa que cubre a todos.
	mustExec(t, `INSERT INTO maintenance_windows (name, starts_at, ends_at, scope_type)
		VALUES ('ventana', now() - interval '1 hour', now() + interval '1 hour', 'all')`)

	// candidateRow es la fila comparable: value y detail se pasan por texto (NULL -> "\x00") para poder comparar
	// por igualdad de struct en vez de por punteros, que nunca son iguales entre dos consultas distintas.
	type candidateRow struct {
		breach, clear, inMaintenance bool
		detail, value                string
	}
	const isNull = "<null>"
	// Ambas consultas van en la misma transaccion: now() es estable dentro de una transaccion, y sin esto
	// no_data y reboot_pending (que miden segundos desde una fecha) difieren en un puñado de milisegundos entre
	// una consulta y la otra.
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil {
			t.Errorf("rollback: %v", err)
		}
	}()
	query := func(view string) map[string]candidateRow {
		rows, err := tx.Query(ctx, `SELECT rule_id::text, host_id::text, breach, clear, in_maintenance,
			COALESCE(detail, $1), COALESCE(round(value::numeric, 4)::text, $1) FROM `+view, isNull)
		if err != nil {
			t.Fatalf("consultando %s: %v", view, err)
		}
		defer rows.Close()
		out := map[string]candidateRow{}
		for rows.Next() {
			var ruleID, hostID, detail, value string
			var row candidateRow
			if err := rows.Scan(&ruleID, &hostID, &row.breach, &row.clear, &row.inMaintenance, &detail, &value); err != nil {
				t.Fatal(err)
			}
			row.detail, row.value = detail, value
			out[ruleID+"/"+hostID] = row
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}

	got := query("alert_candidates")
	want := query("alert_candidates_legacy")
	if len(got) != len(want) {
		t.Fatalf("cantidad de candidatos got %d, want %d", len(got), len(want))
	}
	if len(got) == 0 {
		t.Fatal("el escenario no genero ningun candidato: revisar el seed")
	}
	for k, w := range want {
		g, ok := got[k]
		if !ok {
			t.Errorf("falta el candidato %s en la vista nueva, want %+v", k, w)
			continue
		}
		if g != w {
			t.Errorf("candidato %s got %+v, want %+v", k, g, w)
		}
	}
	for k, g := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("candidato %s sobra en la vista nueva: %+v", k, g)
		}
	}
}
