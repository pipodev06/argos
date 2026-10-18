package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
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
