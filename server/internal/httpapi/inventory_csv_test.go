package httpapi

import (
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// csvBody valida cabeceras/BOM del export de inventario y devuelve cabecera y filas.
func csvBody(t *testing.T, rec *httptest.ResponseRecorder, filename string) ([]string, [][]string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s got %d %s", filename, rec.Code, rec.Body.String())
	}
	h := rec.Header()
	if h.Get("Content-Type") != "text/csv; charset=utf-8" || h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Cache-Control") != "no-store" {
		t.Errorf("%s cabeceras got %v", filename, h)
	}
	if !regexp.MustCompile(`^attachment; filename="argos-` + filename + `-\d{8}\.csv"$`).MatchString(h.Get("Content-Disposition")) {
		t.Errorf("%s Content-Disposition got %q", filename, h.Get("Content-Disposition"))
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "\xEF\xBB\xBF") {
		t.Fatalf("%s sin BOM UTF-8", filename)
	}
	all, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\xEF\xBB\xBF"))).ReadAll()
	if err != nil || len(all) == 0 {
		t.Fatalf("%s csv invalido: %v (%d filas)", filename, err, len(all))
	}
	return all[0], all[1:]
}

// Los exports de inventario respetan el ambito del viewer (sin datos de otros clientes), neutralizan formulas del
// hostname y honran los filtros de la lista.
func TestInventoryCSVScopeInjectionAndFilters(t *testing.T) {
	x := newAlertScopeEnv(t)
	const evil = `=cmd|' /C calc'!A0`
	mustExec(t, `UPDATE hosts SET hostname = $1 WHERE id = $2`, evil, x.hostA)

	hdr, rows := csvBody(t, x.do("GET", "/api/v1/hosts.csv", "", withCookie(x.admin)), "inventario-hosts")
	if len(rows) != 3 {
		t.Fatalf("admin hosts.csv got %d filas, want 3", len(rows))
	}
	found := false
	for _, r := range rows {
		if r[col(t, hdr, "hostname")] == "'"+evil {
			found = true
		}
		for _, cell := range r {
			if strings.HasPrefix(cell, "=") {
				t.Errorf("celda con formula sin neutralizar: %q", cell)
			}
		}
	}
	if !found {
		t.Errorf("no aparece el hostname neutralizado %q", "'"+evil)
	}

	// Viewer restringido a A: solo su host y nada ajeno.
	rec := x.do("GET", "/api/v1/hosts.csv", "", withCookie(x.va))
	_, vrows := csvBody(t, rec, "inventario-hosts")
	if len(vrows) != 1 {
		t.Errorf("viewer hosts.csv got %d filas, want 1", len(vrows))
	}
	for _, leak := range []string{x.hostB, x.hostNo, x.groupB, "host-b", "host-none"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("hosts.csv del viewer contiene dato ajeno %q", leak)
		}
	}

	// Filtro de la lista: por busqueda de texto solo sale el host que coincide.
	_, frows := csvBody(t, x.do("GET", "/api/v1/hosts.csv?q=host-b", "", withCookie(x.admin)), "inventario-hosts")
	if len(frows) != 1 {
		t.Errorf("hosts.csv?q=host-b got %d filas, want 1", len(frows))
	}

	// Dispositivos: sin dispositivos sembrados salen solo la cabecera, tambien para el viewer.
	_, drows := csvBody(t, x.do("GET", "/api/v1/devices.csv", "", withCookie(x.va)), "inventario-dispositivos")
	if len(drows) != 0 {
		t.Errorf("devices.csv got %d filas, want 0", len(drows))
	}

	// Sin sesion: 401.
	if rec := x.do("GET", "/api/v1/hosts.csv", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("hosts.csv sin sesion got %d, want 401", rec.Code)
	}
}
