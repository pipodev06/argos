package httpapi

import (
	"net/http"
	"testing"
)

// TestBulkEnrollHttpFlow (B16, viaje completo): un token de grupo (max_uses > 1, sin hostname) canjeado por
// varios agentes distintos crea un host por cada uno con SU PROPIO hostname (el que manda en el body de
// /agent/enroll), todos en el grupo del token; agotados los usos, el siguiente canje falla como un token ya
// usado. Cubre el hueco de tests-pendientes.md: antes solo se probo a nivel store + que el cliente manda el
// hostname, no el viaje HTTP real de punta a punta con el servidor.
func TestBulkEnrollHttpFlow(t *testing.T) {
	e := newEnv(t, Options{})
	admin := e.adminCookie()
	groupID := e.newGroup(admin, `{"name":"flota"}`)

	rec := e.do("POST", "/api/v1/hosts/enroll-token", `{"group_id":"`+groupID+`","max_uses":2}`, withCookie(admin))
	e.wantStatus(rec, http.StatusCreated, "")
	var tok struct {
		EnrollToken string `json:"enroll_token"`
	}
	e.dataAs(rec, &tok)

	enroll := func(hostname string) (hostID, agentToken string, status int) {
		rec := e.do("POST", "/api/v1/agent/enroll", `{"enroll_token":"`+tok.EnrollToken+`","os":"linux","arch":"amd64","hostname":"`+hostname+`"}`)
		var enr struct {
			HostID     string `json:"host_id"`
			AgentToken string `json:"agent_token"`
		}
		if rec.Code == http.StatusCreated {
			e.dataAs(rec, &enr)
		}
		return enr.HostID, enr.AgentToken, rec.Code
	}

	h1, tok1, code1 := enroll("maquina-1")
	if code1 != http.StatusCreated {
		t.Fatalf("primer canje got %d, want 201", code1)
	}
	h2, tok2, code2 := enroll("maquina-2")
	if code2 != http.StatusCreated {
		t.Fatalf("segundo canje got %d, want 201", code2)
	}
	if h1 == h2 || h1 == "" || h2 == "" {
		t.Fatalf("dos canjes debieron crear dos hosts distintos: %q, %q", h1, h2)
	}
	e.wantStatus(e.metrics(tok1), http.StatusOK, "")
	e.wantStatus(e.metrics(tok2), http.StatusOK, "")

	for _, id := range []string{h1, h2} {
		var hostname, group string
		if err := testPool.QueryRow(t.Context(), `SELECT h.hostname, g.id::text FROM hosts h JOIN host_groups g ON g.id = h.group_id WHERE h.id = $1`, id).
			Scan(&hostname, &group); err != nil {
			t.Fatalf("host %s: %v", id, err)
		}
		if group != groupID {
			t.Errorf("host %s quedo en el grupo %q, want %q", id, group, groupID)
		}
	}

	// Agotados los 2 usos, el tercer canje falla igual que un token usado.
	if _, _, code3 := enroll("maquina-3"); code3 != http.StatusUnauthorized {
		t.Errorf("tercer canje (usos agotados) got %d, want 401", code3)
	}
}
