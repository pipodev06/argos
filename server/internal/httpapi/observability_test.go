package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// pingStore es un fakeStore cuyo Ping se controla y se cuenta.
type pingStore struct {
	*fakeStore
	err   error
	calls atomic.Int32
}

func (p *pingStore) Ping(context.Context) error {
	p.calls.Add(1)
	return p.err
}

func newHealthAPI(t *testing.T, ps *pingStore) (http.Handler, *logBuffer) {
	t.Helper()
	logs := &logBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	return New(ps, testHasher(), logger, func() time.Time { return fixedNow }, Options{}).Routes(), logs
}

func TestHealthDBUp(t *testing.T) {
	t.Parallel()
	h, _ := newHealthAPI(t, &pingStore{fakeStore: &fakeStore{}})
	rec, _ := do(t, h, "GET", "/health", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Errorf("got %d %s, want 200 ok", rec.Code, rec.Body.String())
	}
}

func TestHealthDBDown(t *testing.T) {
	t.Parallel()
	ps := &pingStore{fakeStore: &fakeStore{}, err: errors.New("dial tcp 10.9.9.9:5432: connect: refused user=argos")}
	h, _ := newHealthAPI(t, ps)
	rec, _ := do(t, h, "GET", "/health", "", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status got %d, want 503", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "10.9.9.9") || strings.Contains(body, "argos") || !strings.Contains(body, "unavailable") {
		t.Errorf("el cuerpo no debe traer detalles internos: %s", body)
	}
}

func TestHealthCachesThePing(t *testing.T) {
	t.Parallel()
	ps := &pingStore{fakeStore: &fakeStore{}}
	h, _ := newHealthAPI(t, ps)
	for range 20 {
		if rec, _ := do(t, h, "GET", "/health", "", ""); rec.Code != http.StatusOK {
			t.Fatalf("status got %d", rec.Code)
		}
	}
	if n := ps.calls.Load(); n != 1 {
		t.Errorf("pings a la BD got %d, want 1 (cache de %s)", n, healthTTL)
	}
}

func TestRequestLogHasNoSecrets(t *testing.T) {
	t.Parallel()
	h, logs := newTestAPI(t, &fakeStore{})
	do(t, h, "GET", "/api/v1/hosts?token=SECRETQUERY&q=SECRETSEARCH", "SECRETBEARER", "", func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "__Host-argos_sid", Value: "SECRETCOOKIE"})
	})
	out := logs.String()
	for _, secret := range []string{"SECRETQUERY", "SECRETSEARCH", "SECRETBEARER", "SECRETCOOKIE"} {
		if strings.Contains(out, secret) {
			t.Errorf("el log de solicitudes filtra %s:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, `"path":"/api/v1/hosts"`) || !strings.Contains(out, `"status":401`) || !strings.Contains(out, `"level":"WARN"`) {
		t.Errorf("falta la linea de solicitud (ruta sin query, 401, WARN):\n%s", out)
	}
}

func TestRequestLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path   string
		status int
		want   slog.Level
	}{
		{"/health", 200, slog.LevelDebug},
		{"/health", 503, slog.LevelError},
		{"/api/v1/agent/metrics", 204, slog.LevelDebug},
		{"/api/v1/agent/metrics", 401, slog.LevelWarn},
		{"/assets/index.js", 200, slog.LevelDebug},
		{"/dl/latest/manifest.json", 200, slog.LevelDebug},
		{"/api/v1/hosts", 200, slog.LevelInfo},
		{"/api/v1/hosts", 404, slog.LevelWarn},
		{"/api/v1/hosts", 500, slog.LevelError},
	}
	for _, tc := range tests {
		if got := requestLevel(tc.path, tc.status); got != tc.want {
			t.Errorf("requestLevel(%s, %d) = %v, want %v", tc.path, tc.status, got, tc.want)
		}
	}
}

func TestRequestLogTruncatesLongPath(t *testing.T) {
	t.Parallel()
	h, logs := newTestAPI(t, &fakeStore{})
	req := httptest.NewRequest("GET", "/api/v1/"+strings.Repeat("a", 5000), nil)
	req.Host = "localhost:8080"
	h.ServeHTTP(httptest.NewRecorder(), req)
	if n := len(logs.String()); n > 1500 {
		t.Errorf("el log de una ruta enorme mide %d bytes, se esperaba acotado", n)
	}
}
