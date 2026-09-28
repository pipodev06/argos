package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

const testMetricsToken = "0123456789abcdef0123456789abcdef-token"

func TestMetricsEndpoint(t *testing.T) {
	t.Run("sin token configurado responde 404", func(t *testing.T) {
		e := newEnv(t, Options{})
		e.wantStatus(e.do("GET", "/metrics", "", withHeader("Authorization", "Bearer "+testMetricsToken)), http.StatusNotFound, "")
	})

	e := newEnv(t, Options{MetricsToken: testMetricsToken, Status: StatusSources{Version: "v-test", StartedAt: time.Now()}})
	admin := e.adminCookie()
	if _, err := testPool.Exec(context.Background(), `INSERT INTO host_groups (name) VALUES ('cliente-secreto-xyz')`); err != nil {
		t.Fatalf("sembrando grupo: %v", err)
	}
	e.enrollHost(admin, "host-secreto-abc")

	for name, hdr := range map[string]string{
		"sin header":           "",
		"token incorrecto":     "Bearer " + strings.Repeat("x", len(testMetricsToken)),
		"esquema equivocado":   "Basic " + testMetricsToken,
		"token con prefijo":    "Bearer " + testMetricsToken + "x",
		"solo el token, crudo": testMetricsToken,
	} {
		t.Run("401 "+name, func(t *testing.T) {
			var opts []reqOpt
			if hdr != "" {
				opts = append(opts, withHeader("Authorization", hdr))
			}
			rec := e.do("GET", "/metrics", "", opts...)
			e.wantStatus(rec, http.StatusUnauthorized, "")
			if rec.Body.Len() != 0 {
				t.Errorf("cuerpo del 401 got %q, want vacio", rec.Body.String())
			}
		})
	}

	t.Run("200 con token correcto y sin datos de clientes", func(t *testing.T) {
		rec := e.do("GET", "/metrics", "", withHeader("Authorization", "Bearer "+testMetricsToken))
		e.wantStatus(rec, http.StatusOK, "")
		if ct := rec.Header().Get("Content-Type"); ct != metricsContentType {
			t.Errorf("Content-Type got %q, want %q", ct, metricsContentType)
		}
		body := rec.Body.String()
		for _, want := range []string{
			`argos_build_info{version="v-test"} 1`, "argos_uptime_seconds ", "argos_db_size_bytes ",
			`argos_hosts{status="up"} 1`, `argos_alerts_open{severity="critical"} 0`,
			"argos_notification_queue_pending 0", "argos_notification_dead_24h 0",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("falta %q en:\n%s", want, body)
			}
		}
		for _, secret := range []string{"host-secreto-abc", "cliente-secreto-xyz", testMetricsToken} {
			if strings.Contains(body, secret) {
				t.Errorf("el cuerpo filtra %q", secret)
			}
		}
	})
}
