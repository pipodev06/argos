package httpapi

import (
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"argos/server/internal/store"
)

func TestCSVSafe(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{`=HYPERLINK("http://x","y")`, `'=HYPERLINK("http://x","y")`},
		{"+1", "'+1"},
		{"-2", "'-2"},
		{"@SUM(A1)", "'@SUM(A1)"},
		{"\t=x", "'\t=x"},
		{"\r=x", "'\r=x"},
		{" =x", "' =x"},
		{"host-1", "host-1"},
		{"a=b", "a=b"},
		{"", ""},
		{"   ", "   "},
	} {
		if got := csvSafe(tc.in); got != tc.want {
			t.Errorf("csvSafe(%q) got %q, want %q", tc.in, got, tc.want)
		}
	}
}

// parseCSV valida cabeceras, BOM y estructura, y devuelve las filas (sin la de titulos).
func parseCSV(t *testing.T, rec *httptest.ResponseRecorder, kind string) (header []string, rows [][]string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s got %d %s", kind, rec.Code, rec.Body.String())
	}
	h := rec.Header()
	if h.Get("Content-Type") != "text/csv; charset=utf-8" || h.Get("Cache-Control") != "no-store" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("%s cabeceras got %v", kind, h)
	}
	if !regexp.MustCompile(`^attachment; filename="argos-` + kind + `-(7d|30d|90d)-\d{8}\.csv"$`).MatchString(h.Get("Content-Disposition")) {
		t.Errorf("%s Content-Disposition got %q", kind, h.Get("Content-Disposition"))
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "\xEF\xBB\xBF") {
		t.Fatalf("%s sin BOM UTF-8", kind)
	}
	all, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\xEF\xBB\xBF"))).ReadAll() // exige columnas constantes
	if err != nil || len(all) == 0 {
		t.Fatalf("%s csv invalido: %v (%d filas)", kind, err, len(all))
	}
	return all[0], all[1:]
}

func col(t *testing.T, header []string, name string) int {
	t.Helper()
	for i, h := range header {
		if h == name {
			return i
		}
	}
	t.Fatalf("falta la columna %q en %v", name, header)
	return -1
}

func TestCSVExportsFormatInjectionScopeAndAudit(t *testing.T) {
	x := newAlertScopeEnv(t)
	const evil = `=cmd|' /C calc'!A0`
	x.clock.Advance(time.Minute) // las alertas sembradas nacen con now() de la BD, posterior al reloj falso
	mustExec(t, `UPDATE hosts SET hostname = $1 WHERE id = $2`, evil, x.hostA)
	mustExec(t, `INSERT INTO service_check_defs (id, name, type, target_host, target_port) VALUES (gen_random_uuid(), '+web', 'tcp', 'localhost', 22)`)
	mustExec(t, `INSERT INTO service_check_events (host_id, check_id, from_status, to_status, at)
		SELECT h, d.id, NULL, 'up', now() - interval '1 hour' FROM service_check_defs d, unnest($1::uuid[]) h`, []string{x.hostA, x.hostB})

	base := "/api/v1/reports/"
	// Admin: todo, con la formula del hostname neutralizada en los tres CSV.
	for kind, path := range map[string]string{"disponibilidad": "availability.csv?range=7d", "chequeos": "service-checks.csv", "alertas": "alerts.csv?range=7d&state=open"} {
		hdr, rows := parseCSV(t, x.do("GET", base+path, "", withCookie(x.admin)), kind)
		wantRows := map[string]int{"disponibilidad": 3, "chequeos": 2, "alertas": 3}[kind]
		if len(rows) != wantRows {
			t.Errorf("%s admin got %d filas, want %d", kind, len(rows), wantRows)
		}
		found := false
		for _, r := range rows {
			if r[col(t, hdr, "hostname")] == "'"+evil {
				found = true
			}
			for _, cell := range r {
				if strings.HasPrefix(cell, "=") {
					t.Errorf("%s: celda con formula sin neutralizar: %q", kind, cell)
				}
			}
		}
		if !found {
			t.Errorf("%s: no aparece el hostname neutralizado %q", kind, "'"+evil)
		}
	}
	// El nombre del chequeo tambien va por csvSafe.
	hdr, rows := parseCSV(t, x.do("GET", base+"service-checks.csv", "", withCookie(x.admin)), "chequeos")
	if got := rows[0][col(t, hdr, "chequeo")]; got != "'+web" {
		t.Errorf("chequeo got %q, want %q", got, "'+web")
	}
	// Numeros sin comillas: los segundos son enteros.
	hdr, rows = parseCSV(t, x.do("GET", base+"availability.csv", "", withCookie(x.admin)), "disponibilidad")
	if v := rows[0][col(t, hdr, "up_s")]; strings.HasPrefix(v, "'") {
		t.Errorf("up_s no debe pasar por csvSafe: %q", v)
	}

	// Viewer restringido a A: solo su host, sin rastro de datos ajenos, en los tres.
	for kind, path := range map[string]string{"disponibilidad": "availability.csv", "chequeos": "service-checks.csv", "alertas": "alerts.csv"} {
		rec := x.do("GET", base+path, "", withCookie(x.va))
		_, rows := parseCSV(t, rec, kind)
		if len(rows) != 1 {
			t.Errorf("%s viewer got %d filas, want 1", kind, len(rows))
		}
		for _, leak := range []string{x.hostB, x.hostNo, x.groupB, "host-b", "host-none"} {
			if strings.Contains(rec.Body.String(), leak) {
				t.Errorf("%s del viewer contiene dato ajeno %q", kind, leak)
			}
		}
		// group_id ajeno, malformado e inexistente: 404 identico; el propio funciona.
		missing := x.do("GET", base+path+"?group_id="+missingID, "", withCookie(x.va))
		for _, gid := range []string{x.groupB, "xx"} {
			r := x.do("GET", base+path+"?group_id="+gid, "", withCookie(x.va))
			if r.Code != http.StatusNotFound || r.Body.String() != missing.Body.String() {
				t.Errorf("%s group_id %q got %d %q, want 404 identico", kind, gid, r.Code, r.Body.String())
			}
		}
		if r := x.do("GET", base+path+"?group_id="+x.groupA, "", withCookie(x.va)); r.Code != http.StatusOK {
			t.Errorf("%s group_id propio got %d", kind, r.Code)
		}
	}

	// Parametros invalidos y sin sesion.
	for _, q := range []string{"availability.csv?range=1d", "alerts.csv?state=x"} {
		if rec := x.do("GET", base+q, "", withCookie(x.admin)); rec.Code != http.StatusBadRequest {
			t.Errorf("%s got %d, want 400", q, rec.Code)
		}
	}
	if rec := x.do("GET", base+"alerts.csv", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("sin sesion got %d, want 401", rec.Code)
	}

	// Sin alertas en el rango: CSV solo con la cabecera.
	mustExec(t, `UPDATE alerts SET opened_at = now() - interval '200 days', state = 'resolved', resolved_at = now() - interval '199 days', resolve_reason = 'manual'`)
	if _, rows := parseCSV(t, x.do("GET", base+"alerts.csv?range=90d", "", withCookie(x.admin)), "alertas"); len(rows) != 0 {
		t.Errorf("alertas fuera de rango got %d filas, want 0", len(rows))
	}

	// Auditoria: una entrada ok por exportacion valida, con tipo/rango/filas y sin contenido (ni hostnames).
	if n := x.count(`SELECT count(*) FROM audit_log WHERE event = 'report_exported' AND outcome = 'ok' AND actor_username = 'admin'
		AND detail->>'report' = 'alerts' AND detail->>'range' = '7d' AND (detail->>'rows')::int = 3`); n != 1 {
		t.Errorf("auditoria de alerts.csv del admin got %d, want 1", n)
	}
	if n := x.count(`SELECT count(*) FROM audit_log WHERE event = 'report_exported' AND outcome = 'ok' AND actor_username = 'viewa'
		AND detail->>'report' = 'availability' AND detail->>'group_id' IS NULL AND (detail->>'rows')::int = 1`); n != 1 {
		t.Errorf("auditoria de availability.csv del viewer got %d, want 1", n)
	}
	if n := x.count(`SELECT count(*) FROM audit_log WHERE event = 'report_exported' AND (detail::text LIKE '%cmd%' OR detail::text LIKE '%host-%')`); n != 0 {
		t.Errorf("la auditoria contiene contenido exportado (%d)", n)
	}
	if n := x.count(`SELECT count(*) FROM audit_log WHERE event = 'report_exported' AND detail->>'group_id' = $1`, x.groupB); n != 0 {
		t.Errorf("un 404 por scope no debe auditarse como exportacion (%d)", n)
	}
}

func TestCSVAlertsRowCapAnd422BeforeWriting(t *testing.T) {
	x := newAlertScopeEnv(t)
	mustExec(t, `DELETE FROM alerts`)
	insert := func(n int) {
		mustExec(t, `INSERT INTO alerts (host_id, rule_name, rule_kind, severity, state, opened_at, resolved_at, resolve_reason)
			SELECT $1::uuid, 'r', 'metric', 'info', 'resolved', now() - interval '1 hour' - (g || ' seconds')::interval,
			       now() - interval '30 minutes', 'recovered' FROM generate_series(1, $2) g`, x.hostA, n)
	}
	insert(store.MaxExportRows)
	rec := x.do("GET", "/api/v1/reports/alerts.csv?range=7d", "", withCookie(x.admin))
	if _, rows := parseCSV(t, rec, "alertas"); len(rows) != store.MaxExportRows {
		t.Fatalf("en el tope exacto got %d filas, want %d", len(rows), store.MaxExportRows)
	}
	insert(1)
	rec = x.do("GET", "/api/v1/reports/alerts.csv?range=7d", "", withCookie(x.admin))
	x.wantStatus(rec, http.StatusUnprocessableEntity, "export_too_large")
	if strings.Contains(rec.Header().Get("Content-Type"), "csv") || strings.HasPrefix(rec.Body.String(), "\xEF\xBB\xBF") {
		t.Errorf("el 422 no debe empezar a escribir el CSV: %s %.40q", rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if n := x.count(`SELECT count(*) FROM audit_log WHERE event = 'report_exported' AND outcome = 'fail' AND detail->>'reason' = 'too_large'`); n != 1 {
		t.Errorf("auditoria del tope got %d, want 1", n)
	}
	// Acotando por estado (ninguna abierta) vuelve a caber.
	if _, rows := parseCSV(t, x.do("GET", "/api/v1/reports/alerts.csv?range=7d&state=open", "", withCookie(x.admin)), "alertas"); len(rows) != 0 {
		t.Errorf("state=open got %d filas, want 0", len(rows))
	}
}

func TestCSVSharesReportConcurrencyLimit(t *testing.T) {
	x := newScopeEnv(t)
	for range maxConcurrentReports {
		x.api.reportSem <- struct{}{}
	}
	for _, p := range []string{"availability.csv", "service-checks.csv", "alerts.csv"} {
		rec := x.do("GET", "/api/v1/reports/"+p, "", withCookie(x.admin))
		if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
			t.Errorf("%s con cupos llenos got %d, want 429 con Retry-After", p, rec.Code)
		}
	}
	for range maxConcurrentReports {
		<-x.api.reportSem
	}
	if rec := x.do("GET", "/api/v1/reports/alerts.csv", "", withCookie(x.admin)); rec.Code != http.StatusOK || len(x.api.reportSem) != 0 {
		t.Errorf("con cupos libres got %d (ocupados %d), want 200 y cupo liberado", rec.Code, len(x.api.reportSem))
	}
}
