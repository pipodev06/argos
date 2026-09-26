package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"argos/server/internal/agentdist"
	"argos/server/internal/store"
)

// Lote L: B9 versiones del agente y B10 tokens de enrolamiento pendientes.

func TestAgentOutdated(t *testing.T) {
	t.Parallel()
	tests := []struct {
		version, latest string
		want            bool
	}{
		{"1.2.0", "1.2.0", false},
		{"1.1.9", "1.2.0", true},
		{"1.10.0", "1.9.0", false}, // comparacion numerica, no de texto
		{"1.9.0", "1.10.0", true},
		{"2.0.0", "1.2.0", false}, // por delante del catalogo
		{"v1.2.0", "1.2.0", false},
		{"1.2", "1.2.0", false},
		{"1.2.0-rc1", "1.2.0", true},
		{"1.2.0", "1.2.0-rc1", false},
		{"1.2.0+build5", "1.2.0", false},
		{"dev", "1.2.0", false},
		{"", "1.2.0", false},
		{"raro!", "1.2.0", true},      // ilegible y distinta
		{"1.2.0", "cualquiera", true}, // el catalogo no es numerico: basta que difiera
		{"1.2.3.4", "1.2.0", true},
		{"-1.2.0", "1.2.0", true},
	}
	for _, tt := range tests {
		if got := agentOutdated(tt.version, tt.latest); got != tt.want {
			t.Errorf("agentOutdated(%q, %q) got %v, want %v", tt.version, tt.latest, got, tt.want)
		}
	}
}

func TestAgentVersionsInSummaryAndHostFilter(t *testing.T) {
	x := newAlertScopeEnvWith(t, Options{HostDownAfter: 2 * time.Minute, Dist: &agentdist.Catalog{Version: "1.2.0"}})
	// host-a (grupo A) 1.2.0, host-b (grupo B) 1.1.0, host-none dev; un dispositivo no cuenta.
	for host, ver := range map[string]string{x.hostA: "1.2.0", x.hostB: "1.1.0", x.hostNo: "dev"} {
		mustExec(t, `INSERT INTO host_info (host_id, agent_version) VALUES ($1, $2)
			ON CONFLICT (host_id) DO UPDATE SET agent_version = EXCLUDED.agent_version`, host, ver)
	}

	type summary struct {
		AgentVersions struct {
			Latest *string `json:"latest"`
			Counts []struct {
				Version string `json:"version"`
				Hosts   int    `json:"hosts"`
			} `json:"counts"`
		} `json:"agent_versions"`
	}
	sum := func(c *http.Cookie) summary {
		t.Helper()
		var s summary
		x.dataAs(x.do("GET", "/api/v1/summary", "", withCookie(c)), &s)
		return s
	}
	counts := func(s summary) []string {
		var out []string
		for _, c := range s.AgentVersions.Counts {
			out = append(out, fmt.Sprintf("%s=%d", c.Version, c.Hosts))
		}
		return out
	}
	admin := sum(x.admin)
	if admin.AgentVersions.Latest == nil || *admin.AgentVersions.Latest != "1.2.0" {
		t.Errorf("latest got %v, want 1.2.0", admin.AgentVersions.Latest)
	}
	if got, want := counts(admin), []string{"1.1.0=1", "1.2.0=1", "dev=1"}; !slices.Equal(got, want) {
		t.Errorf("conteos del admin got %v, want %v", got, want)
	}
	// El viewer restringido a A solo cuenta lo suyo: no se filtran las versiones de otros clientes.
	if got, want := counts(sum(x.va)), []string{"1.2.0=1"}; !slices.Equal(got, want) {
		t.Errorf("conteos del viewer de A got %v, want %v", got, want)
	}

	// La lista trae agent_version por host y ?agent=outdated deja solo los desactualizados (no dev) del ambito.
	var page struct {
		Items []struct {
			ID           string  `json:"id"`
			AgentVersion *string `json:"agent_version"`
		} `json:"items"`
	}
	x.dataAs(x.do("GET", "/api/v1/hosts?sort=name", "", withCookie(x.admin)), &page)
	got := map[string]string{}
	for _, it := range page.Items {
		if it.AgentVersion != nil {
			got[it.ID] = *it.AgentVersion
		}
	}
	if got[x.hostA] != "1.2.0" || got[x.hostB] != "1.1.0" || got[x.hostNo] != "dev" {
		t.Errorf("agent_version por host got %v", got)
	}
	if p := x.list(x.admin, "agent=outdated"); p.Total != 1 || p.Items[0].ID != x.hostB {
		t.Errorf("agent=outdated del admin got %+v, want solo host-b", p)
	}
	if p := x.list(x.va, "agent=outdated"); p.Total != 0 {
		t.Errorf("agent=outdated del viewer de A got %+v, want vacio (host-b no es suyo)", p)
	}
	x.wantStatus(x.do("GET", "/api/v1/hosts?agent=otro", "", withCookie(x.admin)), http.StatusBadRequest, "invalid_filter")

	// Un host que sube a la ultima deja de aparecer; el filtro compone con los demas y con el cursor.
	mustExec(t, `UPDATE host_info SET agent_version = '1.2.0' WHERE host_id = $1`, x.hostB)
	if p := x.list(x.admin, "agent=outdated"); p.Total != 0 {
		t.Errorf("tras actualizar got %+v, want vacio", p)
	}
}

func TestOutdatedFilterWithoutDistCatalogIsEmpty(t *testing.T) {
	x := newScopeEnv(t)
	mustExec(t, `INSERT INTO host_info (host_id, agent_version) VALUES ($1, '0.0.1')`, x.hostA)
	var s struct {
		AgentVersions struct {
			Latest *string `json:"latest"`
		} `json:"agent_versions"`
	}
	x.dataAs(x.do("GET", "/api/v1/summary", "", withCookie(x.admin)), &s)
	if s.AgentVersions.Latest != nil {
		t.Errorf("latest sin catalogo got %v, want null", *s.AgentVersions.Latest)
	}
	if p := x.list(x.admin, "agent=outdated"); p.Total != 0 {
		t.Errorf("sin catalogo no hay nada que comparar: got %+v, want vacio", p)
	}
}

// B10: tokens de enrolamiento pendientes.
func TestPendingEnrollTokens(t *testing.T) {
	x := newScopeEnv(t)
	create := func(hostname, group string) (token string) {
		t.Helper()
		body := `{"hostname":"` + hostname + `"`
		if group != "" {
			body += `,"group_id":"` + group + `"`
		}
		rec := x.do("POST", "/api/v1/hosts/enroll-token", body+`}`, withCookie(x.admin))
		x.wantStatus(rec, http.StatusCreated, "")
		var d struct {
			Token string `json:"enroll_token"`
		}
		x.dataAs(rec, &d)
		return d.Token
	}
	// Vigentes: pend-1 (con grupo A) y pend-2. Usado: x.hostA..hostNo se enrolaron en la siembra. Expirado: exp.
	tok1 := create("pend-1", x.groupA)
	tok2 := create("pend-2", "")
	create("expirado", "")
	mustExec(t, `UPDATE enroll_tokens SET expires_at = now() - interval '1 minute' WHERE hostname = 'expirado'`)

	type item struct {
		ID        string  `json:"id"`
		Hostname  string  `json:"hostname"`
		CreatedBy *string `json:"created_by"`
		CreatedAt string  `json:"created_at"`
		ExpiresAt string  `json:"expires_at"`
		Group     *struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"group"`
	}
	list := func(c *http.Cookie) (items []item, body string) {
		t.Helper()
		rec := x.do("GET", "/api/v1/hosts/enroll-tokens", "", withCookie(c))
		x.wantStatus(rec, http.StatusOK, "")
		var d struct {
			Items []item `json:"items"`
		}
		x.dataAs(rec, &d)
		return d.Items, rec.Body.String()
	}
	items, body := list(x.admin)
	names := map[string]item{}
	for _, it := range items {
		names[it.Hostname] = it
	}
	if len(items) != 2 || names["pend-1"].ID == "" || names["pend-2"].ID == "" {
		t.Fatalf("pendientes got %+v, want solo pend-1 y pend-2 (ni usados ni expirados)", items)
	}
	p1 := names["pend-1"]
	if p1.Group == nil || p1.Group.ID != x.groupA || p1.Group.Name != "A" || p1.CreatedBy == nil || *p1.CreatedBy != "admin" || p1.CreatedAt == "" || p1.ExpiresAt == "" {
		t.Errorf("pend-1 got %+v", p1)
	}
	if names["pend-2"].Group != nil {
		t.Errorf("pend-2 no tiene grupo: got %+v", names["pend-2"].Group)
	}
	// Nunca el token ni su hash (ni completo ni el prefijo).
	for _, tok := range []string{tok1, tok2} {
		sum := sha256.Sum256([]byte(tok))
		for _, leak := range []string{tok, hex.EncodeToString(sum[:]), hex.EncodeToString(sum[:])[:16]} {
			if strings.Contains(body, leak) {
				t.Errorf("la lista expone %q", leak)
			}
		}
	}
	if strings.Contains(body, "token") {
		t.Errorf("la lista no debe tener campos con 'token': %s", body)
	}

	// Viewer (incluso restringido): 403 en listar y revocar.
	x.wantStatus(x.do("GET", "/api/v1/hosts/enroll-tokens", "", withCookie(x.va)), http.StatusForbidden, "forbidden")
	x.wantStatus(x.do("DELETE", "/api/v1/hosts/enroll-tokens/"+p1.ID, "", withCookie(x.va)), http.StatusForbidden, "forbidden")
	x.wantStatus(x.do("DELETE", "/api/v1/hosts/enroll-tokens/no-uuid", "", withCookie(x.admin)), http.StatusBadRequest, "")

	// Revocar: el token deja de servir, desaparece de la lista, queda auditado y no se puede revocar dos veces.
	x.wantStatus(x.do("DELETE", "/api/v1/hosts/enroll-tokens/"+p1.ID, "", withCookie(x.admin)), http.StatusOK, "")
	x.wantStatus(x.do("POST", "/api/v1/agent/enroll", `{"enroll_token":"`+tok1+`","os":"linux","arch":"amd64"}`), http.StatusUnauthorized, "invalid_token")
	if items, _ := list(x.admin); len(items) != 1 || items[0].Hostname != "pend-2" {
		t.Errorf("tras revocar got %+v, want solo pend-2", items)
	}
	x.wantStatus(x.do("DELETE", "/api/v1/hosts/enroll-tokens/"+p1.ID, "", withCookie(x.admin)), http.StatusNotFound, "not_found")
	if n := x.count(`SELECT count(*) FROM audit_log WHERE event = 'enroll_token_revoked' AND actor_username = 'admin' AND detail->>'hostname' = 'pend-1'`); n != 1 {
		t.Errorf("auditoria enroll_token_revoked got %d filas, want 1", n)
	}

	// Un token ya canjeado o expirado no se revoca por aqui (404), aunque se conozca su id.
	var usedID, expID string
	if err := testPool.QueryRow(t.Context(), `SELECT id::text FROM enroll_tokens WHERE hostname = 'host-a'`).Scan(&usedID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(t.Context(), `SELECT id::text FROM enroll_tokens WHERE hostname = 'expirado'`).Scan(&expID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{usedID, expID} {
		x.wantStatus(x.do("DELETE", "/api/v1/hosts/enroll-tokens/"+id, "", withCookie(x.admin)), http.StatusNotFound, "not_found")
	}
}

// B11 (store): no se borra al ultimo admin activo ni a uno mismo.
func TestStoreDeleteUserGuards(t *testing.T) {
	x := newScopeEnv(t)
	admin := x.newUser("otro-admin", pwAdmin, store.RoleAdmin, false)
	var first string
	if err := testPool.QueryRow(t.Context(), `SELECT id::text FROM users WHERE username = 'admin'`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	viewer := x.newUser("solo-viewer", pwViewer, store.RoleViewer, false)

	// Un viewer (actor ficticio) no puede llevarse al primer admin si el otro esta deshabilitado: quedaria sin admin.
	mustExec(t, `UPDATE users SET disabled_at = now() WHERE id = $1`, admin.ID)
	if _, err := x.st.DeleteUser(t.Context(), first, viewer.ID); !errors.Is(err, store.ErrLastAdmin) {
		t.Errorf("borrar al ultimo admin activo got %v, want ErrLastAdmin", err)
	}
	if _, err := x.st.DeleteUser(t.Context(), first, first); !errors.Is(err, store.ErrSelfDelete) {
		t.Errorf("borrarse a si mismo got %v, want ErrSelfDelete", err)
	}
	// Un admin deshabilitado si se puede borrar (no cuenta como activo).
	if _, err := x.st.DeleteUser(t.Context(), admin.ID, first); err != nil {
		t.Errorf("borrar un admin deshabilitado got %v, want nil", err)
	}
}
