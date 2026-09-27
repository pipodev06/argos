package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"net/http"
	"strings"
	"testing"
)

// TestGroupExportZip: B17, exportar el cliente antes de purgarlo. Un admin obtiene un zip con hosts.csv
// (hosts y dispositivos del grupo) y disponibilidad.csv (mismo informe existente); 404 si el grupo no existe;
// auditoria con el conteo de filas.
func TestGroupExportZip(t *testing.T) {
	x := newScopeEnv(t)
	seedHostData(t, x.hostA)
	devA := x.newDevice(`{"name":"sw-a","address":"10.1.0.1","collector_id":"` + x.hostA + `","group_id":"` + x.groupA + `"}`)

	x.wantStatus(x.do("GET", "/api/v1/groups/no-uuid/export.zip", "", withCookie(x.admin)), http.StatusBadRequest, "invalid_body")
	x.wantStatus(x.do("GET", "/api/v1/groups/"+missingID+"/export.zip", "", withCookie(x.admin)), http.StatusNotFound, "not_found")
	x.wantStatus(x.do("GET", "/api/v1/groups/"+x.groupA+"/export.zip", "", withCookie(x.va)), http.StatusForbidden, "")

	rec := x.do("GET", "/api/v1/groups/"+x.groupA+"/export.zip", "", withCookie(x.admin))
	x.wantStatus(rec, http.StatusOK, "")
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("content-type: %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="cliente-A-`) {
		t.Errorf("content-disposition: %q", cd)
	}

	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("zip invalido: %v", err)
	}
	names := map[string]*zip.File{}
	for _, f := range zr.File {
		names[f.Name] = f
	}
	if _, ok := names["hosts.csv"]; !ok {
		t.Fatal("falta hosts.csv en el zip")
	}
	if _, ok := names["disponibilidad.csv"]; !ok {
		t.Fatal("falta disponibilidad.csv en el zip")
	}

	// hosts.csv: una fila por host y dispositivo del grupo (aqui, colector-agente hostA y devA).
	hf, err := names["hosts.csv"].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hf.Close() }()
	hRows, err := csv.NewReader(hf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(hRows) != 3 { // cabecera + hostA + devA
		t.Fatalf("hosts.csv tiene %d filas, want 3 (cabecera + 2): %v", len(hRows), hRows)
	}
	gotIDs := map[string]bool{hRows[1][0]: true, hRows[2][0]: true}
	if !gotIDs[x.hostA] || !gotIDs[devA] {
		t.Errorf("hosts.csv no trae hostA ni devA: %v", hRows)
	}

	af, err := names["disponibilidad.csv"].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = af.Close() }()
	aRows, err := csv.NewReader(af).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(aRows) < 2 { // cabecera + al menos hostA
		t.Fatalf("disponibilidad.csv tiene %d filas: %v", len(aRows), aRows)
	}

	// Auditoria: conteo de filas, nunca contenido.
	audit := x.do("GET", "/api/v1/audit?event=group_exported", "", withCookie(x.admin)).Body.String()
	if !strings.Contains(audit, "group_exported") || strings.Contains(audit, x.hostA) || strings.Contains(audit, devA) {
		t.Errorf("auditoria del export: %.500s", audit)
	}
}
