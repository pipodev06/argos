package httpapi

import (
	"net/http"
	"testing"
	"time"
)

// B14/B15: re-enrolar un host conservando su id (conserva historial) y reactivar uno disabled, contra Postgres real.

// TestReEnrollHostRoute cubre POST /hosts/{id}/re-enroll: 404 pa host inexistente y dispositivo, 201 con el
// mismo shape que el alta normal pa un host up y pa uno disabled (reactivar).
func TestReEnrollHostRoute(t *testing.T) {
	e := newEnv(t, Options{})
	admin := e.adminCookie()
	hostID, _, _ := e.enrollHost(admin, "srv-01")

	e.wantStatus(e.do("POST", "/api/v1/hosts/"+anyUUID+"/re-enroll", "", withCookie(admin)), http.StatusNotFound, "not_found")
	e.wantStatus(e.do("POST", "/api/v1/hosts/no-uuid/re-enroll", "", withCookie(admin)), http.StatusBadRequest, "")

	// Un dispositivo no se re-enrola asi: 404.
	devRec := e.do("POST", "/api/v1/devices", `{"name":"dev-1","address":"10.0.0.5","collector_id":"`+hostID+`"}`, withCookie(admin))
	e.wantStatus(devRec, http.StatusCreated, "")
	var dev struct {
		Device struct {
			ID string `json:"id"`
		} `json:"device"`
	}
	e.dataAs(devRec, &dev)
	e.wantStatus(e.do("POST", "/api/v1/hosts/"+dev.Device.ID+"/re-enroll", "", withCookie(admin)), http.StatusNotFound, "not_found")

	// Host up: mismo shape que el alta normal.
	rec := e.do("POST", "/api/v1/hosts/"+hostID+"/re-enroll", "", withCookie(admin))
	e.wantStatus(rec, http.StatusCreated, "")
	var resp struct {
		EnrollToken string    `json:"enroll_token"`
		ExpiresAt   time.Time `json:"expires_at"`
	}
	e.dataAs(rec, &resp)
	if len(resp.EnrollToken) != 32 || resp.ExpiresAt.IsZero() {
		t.Errorf("re-enroll got %+v, want enroll_token de 32 y expires_at", resp)
	}

	// Canjear ese token conserva el id del host (no crea uno nuevo).
	before := e.count(`SELECT count(*) FROM hosts`)
	enrollRec := e.do("POST", "/api/v1/agent/enroll", `{"enroll_token":"`+resp.EnrollToken+`","os":"windows","arch":"arm64"}`)
	e.wantStatus(enrollRec, http.StatusCreated, "")
	var enr struct {
		HostID string `json:"host_id"`
	}
	e.dataAs(enrollRec, &enr)
	if enr.HostID != hostID {
		t.Errorf("host_id got %q, want %q (conserva el id)", enr.HostID, hostID)
	}
	if after := e.count(`SELECT count(*) FROM hosts`); after != before {
		t.Errorf("el re-enroll no debe crear un host nuevo: hosts %d->%d", before, after)
	}
}

// TestReEnrollReactivatesDisabledHost cubre B15: reactivar un host disabled por HTTP completo, hasta que
// el agente vuelve a autenticar con el token nuevo (el ingest ya no da 401).
func TestReEnrollReactivatesDisabledHost(t *testing.T) {
	e := newEnv(t, Options{})
	admin := e.adminCookie()
	hostID, oldToken, _ := e.enrollHost(admin, "srv-disabled")

	e.wantStatus(e.do("POST", "/api/v1/hosts/"+hostID+"/revoke", "", withCookie(admin)), http.StatusOK, "")
	e.wantStatus(e.metrics(oldToken), http.StatusUnauthorized, "")

	rec := e.do("POST", "/api/v1/hosts/"+hostID+"/re-enroll", "", withCookie(admin))
	e.wantStatus(rec, http.StatusCreated, "")
	var resp struct {
		EnrollToken string `json:"enroll_token"`
	}
	e.dataAs(rec, &resp)

	enrollRec := e.do("POST", "/api/v1/agent/enroll", `{"enroll_token":"`+resp.EnrollToken+`","os":"linux","arch":"amd64"}`)
	e.wantStatus(enrollRec, http.StatusCreated, "")
	var enr struct {
		HostID     string `json:"host_id"`
		AgentToken string `json:"agent_token"`
	}
	e.dataAs(enrollRec, &enr)
	if enr.HostID != hostID {
		t.Errorf("host_id got %q, want %q", enr.HostID, hostID)
	}
	e.wantStatus(e.metrics(enr.AgentToken), http.StatusOK, "")

	var status string
	if err := testPool.QueryRow(t.Context(), `SELECT status FROM hosts WHERE id = $1`, hostID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "up" {
		t.Errorf("status tras el primer ingest got %q, want up", status)
	}
}

// TestEnrollTokenHostnameConflict cubre el aviso de B14 (no bloqueante) en el alta normal: 201 igual, con
// hostname_conflict presente si ya existe un host con ese hostname y sin el si no.
func TestEnrollTokenHostnameConflict(t *testing.T) {
	e := newEnv(t, Options{})
	admin := e.adminCookie()

	rec := e.do("POST", "/api/v1/hosts/enroll-token", `{"hostname":"srv-nuevo"}`, withCookie(admin))
	e.wantStatus(rec, http.StatusCreated, "")
	var resp struct {
		HostnameConflict *struct {
			HostID string `json:"host_id"`
			Status string `json:"status"`
		} `json:"hostname_conflict"`
	}
	e.dataAs(rec, &resp)
	if resp.HostnameConflict != nil {
		t.Errorf("sin duplicado, hostname_conflict got %+v, want ausente", resp.HostnameConflict)
	}

	hostID, _, _ := e.enrollHost(admin, "srv-dup")
	rec = e.do("POST", "/api/v1/hosts/enroll-token", `{"hostname":"SRV-DUP"}`, withCookie(admin))
	e.wantStatus(rec, http.StatusCreated, "")
	e.dataAs(rec, &resp)
	if resp.HostnameConflict == nil || resp.HostnameConflict.HostID != hostID || resp.HostnameConflict.Status != "up" {
		t.Errorf("con duplicado (case-insensitive) got %+v, want host_id=%q status=up", resp.HostnameConflict, hostID)
	}

	// El alta sigue creando el token igual: no es error, es solo un aviso.
	if got := e.count(`SELECT count(*) FROM enroll_tokens WHERE hostname IN ('srv-nuevo', 'SRV-DUP')`); got != 2 {
		t.Errorf("tokens creados got %d, want 2 (el conflicto no bloquea el alta)", got)
	}
}
