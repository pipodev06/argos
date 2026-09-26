package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"argos/server/internal/chunk"
	"argos/server/internal/store"
)

// insertChunk crea un chunk vacio del host el dia (hoy - k), con "hoy" segun el reloj del test.
func (s *scopeEnv) insertChunk(hostID string, k int) {
	s.t.Helper()
	blob, err := chunk.Encode(nil)
	if err != nil {
		s.t.Fatal(err)
	}
	day := s.clock.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -k)
	_, err = testPool.Exec(context.Background(),
		`INSERT INTO metrics_chunks (host_id, day, n, codec, data) VALUES ($1, $2::date, 0, $3, $4)`, hostID, day, int16(chunk.Version), blob)
	if err != nil {
		s.t.Fatal(err)
	}
}

type shrinkErr struct {
	Error struct {
		Code    string `json:"code"`
		Details struct {
			DaysLost int   `json:"days_lost"`
			Chunks   int   `json:"chunks"`
			EstBytes int64 `json:"est_bytes"`
		} `json:"details"`
	} `json:"error"`
}

func (s *scopeEnv) wantShrink(rec *httptest.ResponseRecorder, daysLost, chunks int) {
	s.t.Helper()
	s.wantStatus(rec, http.StatusConflict, "shrink_needs_confirm")
	var e shrinkErr
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		s.t.Fatal(err)
	}
	d := e.Error.Details
	if d.DaysLost != daysLost || d.Chunks != chunks || d.EstBytes != int64(chunks)*store.EstChunkBytes {
		s.t.Errorf("details = %+v, want days_lost=%d chunks=%d est_bytes=%d", d, daysLost, chunks, int64(chunks)*store.EstChunkBytes)
	}
}

type retentionResp struct {
	DetailDays int    `json:"detail_days"`
	Source     string `json:"source"`
	HotDays    int    `json:"hot_days"`
	HourlyDays int    `json:"hourly_days"`
}

func TestRetentionSettingsAPI(t *testing.T) {
	s := newScopeEnv(t)
	const path = "/api/v1/settings/retention"

	// Solo admin.
	s.wantStatus(s.do("GET", path, ""), http.StatusUnauthorized, "")
	s.wantStatus(s.do("GET", path, "", withCookie(s.va)), http.StatusForbidden, "")
	s.wantStatus(s.do("GET", path, "", withCookie(s.vall)), http.StatusForbidden, "")
	s.wantStatus(s.do("PUT", path, `{"detail_days":60}`, withCookie(s.vall)), http.StatusForbidden, "")

	var got retentionResp
	rec := s.do("GET", path, "", withCookie(s.admin))
	s.wantStatus(rec, http.StatusOK, "")
	s.dataAs(rec, &got)
	if want := (retentionResp{DetailDays: 30, Source: "default", HotDays: 8, HourlyDays: 400}); got != want {
		t.Errorf("GET = %+v, want %+v", got, want)
	}

	// Validacion: 8..730, campo requerido, sin campos desconocidos.
	for _, body := range []string{`{}`, `{"detail_days":7}`, `{"detail_days":731}`, `{"detail_days":null}`, `{"detail_days":"30"}`, `{"detail_days":30,"x":1}`, `{"detail_days":0}`, `{"detail_days":-5}`} {
		s.wantStatus(s.do("PUT", path, body, withCookie(s.admin)), http.StatusBadRequest, "invalid_body")
	}
	for _, days := range []int{8, 730} {
		body, _ := json.Marshal(map[string]int{"detail_days": days})
		s.wantStatus(s.do("PUT", path, string(body), withCookie(s.admin)), http.StatusOK, "")
	}

	// Un cambio valido rige (origen db) y queda auditado sin datos.
	rec = s.do("PUT", path, `{"detail_days":60}`, withCookie(s.admin))
	s.wantStatus(rec, http.StatusOK, "")
	s.dataAs(rec, &got)
	if want := (retentionResp{DetailDays: 60, Source: "db", HotDays: 8, HourlyDays: 400}); got != want {
		t.Errorf("PUT = %+v, want %+v", got, want)
	}
	if n := s.count(`SELECT count(*) FROM audit_log WHERE event = 'retention_updated' AND outcome = 'ok' AND detail = '{"scope":"global","detail_days":60}'::jsonb`); n != 1 {
		t.Errorf("auditoria retention_updated = %d, want 1", n)
	}

	// Reducir lo que borraria detalle existente exige confirm_shrink; subir no.
	s.insertChunk(s.hostNo, 50) // dentro de 60 d, fuera de 45 d
	s.insertChunk(s.hostNo, 10)
	s.wantShrink(s.do("PUT", path, `{"detail_days":45}`, withCookie(s.admin)), 15, 1)
	s.wantShrink(s.do("PUT", path, `{"detail_days":45,"confirm_shrink":false}`, withCookie(s.admin)), 15, 1)
	rec = s.do("GET", path, "", withCookie(s.admin))
	s.dataAs(rec, &got)
	if got.DetailDays != 60 {
		t.Errorf("un 409 no debe cambiar nada: detail_days = %d, want 60", got.DetailDays)
	}
	s.wantStatus(s.do("PUT", path, `{"detail_days":45,"confirm_shrink":true}`, withCookie(s.admin)), http.StatusOK, "")
	// Reducir sin nada que perder (no hay chunks entre 45 y 30 dias) no pide confirmacion.
	s.wantStatus(s.do("PUT", path, `{"detail_days":30}`, withCookie(s.admin)), http.StatusOK, "")
	s.wantStatus(s.do("PUT", path, `{"detail_days":200}`, withCookie(s.admin)), http.StatusOK, "")
}

type retDTO struct {
	Own       *int   `json:"detail_retention_days"`
	Effective int    `json:"effective_detail_days"`
	Source    string `json:"retention_source"`
}

func (s *scopeEnv) hostRetention(hostID string) retDTO {
	s.t.Helper()
	rec := s.do("GET", "/api/v1/hosts/"+hostID, "", withCookie(s.admin))
	s.wantStatus(rec, http.StatusOK, "")
	var d struct {
		Host retDTO `json:"host"`
	}
	s.dataAs(rec, &d)
	return d.Host
}

func (s *scopeEnv) groupRetention(groupID string) retDTO {
	s.t.Helper()
	rec := s.do("GET", "/api/v1/groups", "", withCookie(s.admin))
	s.wantStatus(rec, http.StatusOK, "")
	var d struct {
		Items []struct {
			ID string `json:"id"`
			retDTO
		} `json:"items"`
	}
	s.dataAs(rec, &d)
	for _, g := range d.Items {
		if g.ID == groupID {
			return g.retDTO
		}
	}
	s.t.Fatalf("grupo %s no esta en la lista", groupID)
	return retDTO{}
}

func intp(v int) *int { return &v }

func TestHostAndGroupRetentionAPI(t *testing.T) {
	s := newScopeEnv(t)
	hostURL := func(id string) string { return "/api/v1/hosts/" + id + "/retention" }
	groupURL := func(id string) string { return "/api/v1/groups/" + id }
	check := func(what string, got, want retDTO) {
		t.Helper()
		if (got.Own == nil) != (want.Own == nil) || (got.Own != nil && *got.Own != *want.Own) || got.Effective != want.Effective || got.Source != want.Source {
			t.Errorf("%s: got %+v (own=%v), want %+v (own=%v)", what, got, got.Own, want, want.Own)
		}
	}

	// Estado inicial: todo hereda el global (30, por defecto).
	check("host sin grupo", s.hostRetention(s.hostNo), retDTO{nil, 30, "global"})
	check("grupo", s.groupRetention(s.groupA), retDTO{nil, 30, "global"})

	// Solo admin; id y cuerpo validos; host inexistente 404.
	s.wantStatus(s.do("PUT", hostURL(s.hostNo), `{"detail_retention_days":90}`, withCookie(s.va)), http.StatusForbidden, "")
	s.wantStatus(s.do("PUT", hostURL(s.hostNo), `{"detail_retention_days":90}`), http.StatusUnauthorized, "")
	s.wantStatus(s.do("PUT", hostURL("no-uuid"), `{"detail_retention_days":90}`, withCookie(s.admin)), http.StatusBadRequest, "")
	s.wantStatus(s.do("PUT", hostURL(anyUUID), `{"detail_retention_days":90}`, withCookie(s.admin)), http.StatusNotFound, "not_found")
	for _, body := range []string{`{}`, `{"detail_retention_days":7}`, `{"detail_retention_days":731}`, `{"detail_retention_days":"x"}`, `{"detail_retention_days":90,"otro":1}`} {
		s.wantStatus(s.do("PUT", hostURL(s.hostNo), body, withCookie(s.admin)), http.StatusBadRequest, "invalid_body")
	}

	// Host propio: gana sobre el grupo y el global; null vuelve a heredar.
	rec := s.do("PUT", hostURL(s.hostNo), `{"detail_retention_days":90}`, withCookie(s.admin))
	s.wantStatus(rec, http.StatusOK, "")
	var got retDTO
	s.dataAs(rec, &got)
	check("PUT host", got, retDTO{intp(90), 90, "host"})
	check("host tras fijar", s.hostRetention(s.hostNo), retDTO{intp(90), 90, "host"})

	// Grupo: valida el rango, muestra propio y efectivo, y el host sin valor propio lo hereda.
	for _, body := range []string{`{"detail_retention_days":7}`, `{"detail_retention_days":731}`, `{"detail_retention_days":"x"}`} {
		s.wantStatus(s.do("PATCH", groupURL(s.groupA), body, withCookie(s.admin)), http.StatusBadRequest, "invalid_body")
	}
	s.wantStatus(s.do("PATCH", groupURL(s.groupA), `{"detail_retention_days":20}`, withCookie(s.va)), http.StatusForbidden, "")
	rec = s.do("PATCH", groupURL(s.groupA), `{"detail_retention_days":20}`, withCookie(s.admin))
	s.wantStatus(rec, http.StatusOK, "")
	var g struct {
		Group retDTO `json:"group"`
	}
	s.dataAs(rec, &g)
	check("PUT grupo", g.Group, retDTO{intp(20), 20, "group"})
	check("grupo en la lista", s.groupRetention(s.groupA), retDTO{intp(20), 20, "group"})
	check("host del grupo", s.hostRetention(s.hostA), retDTO{nil, 20, "group"})
	check("otro grupo", s.groupRetention(s.groupB), retDTO{nil, 30, "global"})

	// Otro campo del grupo no toca la retencion; null la quita.
	s.wantStatus(s.do("PATCH", groupURL(s.groupA), `{"always_on":true}`, withCookie(s.admin)), http.StatusOK, "")
	check("grupo tras cambiar otro campo", s.groupRetention(s.groupA), retDTO{intp(20), 20, "group"})
	s.wantStatus(s.do("PATCH", groupURL(s.groupA), `{"detail_retention_days":null}`, withCookie(s.admin)), http.StatusOK, "")
	check("grupo tras null", s.groupRetention(s.groupA), retDTO{nil, 30, "global"})

	// Reducir el grupo B exige confirmacion si hay detalle que se perderia; un host con valor propio no cuenta.
	s.wantStatus(s.do("PATCH", groupURL(s.groupB), `{"detail_retention_days":60}`, withCookie(s.admin)), http.StatusOK, "")
	s.insertChunk(s.hostB, 30)
	s.wantShrink(s.do("PATCH", groupURL(s.groupB), `{"detail_retention_days":20}`, withCookie(s.admin)), 40, 1)
	check("grupo tras el 409", s.groupRetention(s.groupB), retDTO{intp(60), 60, "group"})
	s.wantStatus(s.do("PATCH", groupURL(s.groupB), `{"detail_retention_days":20,"confirm_shrink":true}`, withCookie(s.admin)), http.StatusOK, "")
	check("grupo confirmado", s.groupRetention(s.groupB), retDTO{intp(20), 20, "group"})

	// Host: reducir con detalle que se perderia tambien pide confirmacion (heredaba 20 del grupo B y baja a 10: el chunk de
	// hace 30 dias ya estaba fuera de los 20, solo cuenta el de hace 15).
	s.insertChunk(s.hostB, 15)
	s.wantShrink(s.do("PUT", hostURL(s.hostB), `{"detail_retention_days":10}`, withCookie(s.admin)), 10, 1)
	s.wantStatus(s.do("PUT", hostURL(s.hostB), `{"detail_retention_days":10,"confirm_shrink":true}`, withCookie(s.admin)), http.StatusOK, "")
	check("host confirmado", s.hostRetention(s.hostB), retDTO{intp(10), 10, "host"})
	s.wantStatus(s.do("PUT", hostURL(s.hostB), `{"detail_retention_days":null}`, withCookie(s.admin)), http.StatusOK, "")
	check("host heredando", s.hostRetention(s.hostB), retDTO{nil, 20, "group"})

	// Auditoria: un evento por cambio, sin datos de metricas.
	if n := s.count(`SELECT count(*) FROM audit_log WHERE event = 'retention_updated' AND outcome = 'ok'`); n != 7 {
		t.Errorf("eventos retention_updated = %d, want 7 (un 409 no audita)", n)
	}
	if n := s.count(`SELECT count(*) FROM audit_log WHERE event = 'retention_updated' AND detail->>'scope' = 'host' AND target_id = $1`, s.hostB); n != 2 {
		t.Errorf("eventos del host B = %d, want 2", n)
	}
}
