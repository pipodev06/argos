package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"argos/server/internal/store"
)

const (
	groupUUID = "11111111-1111-4111-8111-111111111111"
	hostUUID  = "0f8fad5b-d9cb-469f-a165-70867728950e"
)

func viewerStore(fs *fakeStore) *fakeStore {
	fs.sessionUser = store.User{ID: "u-viewer", Username: "viewer", Role: store.RoleViewer}
	return fs
}

func dataMap(t *testing.T, resp response) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(resp.Data, &m); err != nil {
		t.Fatalf("data no es un objeto: %v (%s)", err, resp.Data)
	}
	return m
}

func auditEvents(fs *fakeStore) []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	var out []string
	for _, e := range fs.audits {
		out = append(out, e.Event)
	}
	return out
}

func TestGroupCreateUpdateDelete(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", 65)
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		storeErr   error
		wantStatus int
		wantCode   string
	}{
		{"crear ok", "POST", "/api/v1/groups", `{"name":"Laptops","always_on":false}`, nil, 201, ""},
		{"crear con umbral", "POST", "/api/v1/groups", `{"name":"Srv","down_after_seconds":60,"always_on":true}`, nil, 201, ""},
		{"nombre vacio", "POST", "/api/v1/groups", `{"name":"  "}`, nil, 400, "invalid_body"},
		{"nombre largo", "POST", "/api/v1/groups", `{"name":"` + long + `"}`, nil, 400, "invalid_body"},
		{"nombre con control", "POST", "/api/v1/groups", `{"name":"a\nb"}`, nil, 400, "invalid_body"},
		{"umbral bajo", "POST", "/api/v1/groups", `{"name":"x","down_after_seconds":29}`, nil, 400, "invalid_body"},
		{"umbral alto", "POST", "/api/v1/groups", `{"name":"x","down_after_seconds":2592001}`, nil, 400, "invalid_body"},
		{"campo desconocido", "POST", "/api/v1/groups", `{"name":"x","otro":1}`, nil, 400, "invalid_body"},
		{"nombre duplicado", "POST", "/api/v1/groups", `{"name":"x"}`, store.ErrGroupExists, 409, "group_exists"},
		{"limite de grupos", "POST", "/api/v1/groups", `{"name":"x"}`, store.ErrTooManyGroups, 409, "too_many_groups"},
		{"error de db", "POST", "/api/v1/groups", `{"name":"x"}`, errors.New("boom"), 500, "internal_error"},

		{"patch nombre", "PATCH", "/api/v1/groups/" + groupUUID, `{"name":"Nuevo"}`, nil, 200, ""},
		{"patch always_on", "PATCH", "/api/v1/groups/" + groupUUID, `{"always_on":true}`, nil, 200, ""},
		{"patch umbral null", "PATCH", "/api/v1/groups/" + groupUUID, `{"down_after_seconds":null}`, nil, 200, ""},
		{"patch umbral fijo", "PATCH", "/api/v1/groups/" + groupUUID, `{"down_after_seconds":120}`, nil, 200, ""},
		{"patch vacio", "PATCH", "/api/v1/groups/" + groupUUID, `{}`, nil, 400, "invalid_body"},
		{"patch umbral fuera de rango", "PATCH", "/api/v1/groups/" + groupUUID, `{"down_after_seconds":5}`, nil, 400, "invalid_body"},
		{"patch id invalido", "PATCH", "/api/v1/groups/xx", `{"name":"a"}`, nil, 400, "invalid_body"},
		{"patch 404", "PATCH", "/api/v1/groups/" + groupUUID, `{"name":"a"}`, store.ErrNotFound, 404, "not_found"},
		{"patch nombre duplicado", "PATCH", "/api/v1/groups/" + groupUUID, `{"name":"a"}`, store.ErrGroupExists, 409, "group_exists"},

		{"borrar ok", "DELETE", "/api/v1/groups/" + groupUUID, ``, nil, 200, ""},
		{"borrar con hosts", "DELETE", "/api/v1/groups/" + groupUUID, ``, store.ErrGroupNotEmpty, 409, "group_not_empty"},
		{"borrar 404", "DELETE", "/api/v1/groups/" + groupUUID, ``, store.ErrNotFound, 404, "not_found"},
		{"borrar id invalido", "DELETE", "/api/v1/groups/nope", ``, nil, 400, "invalid_body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := &fakeStore{x: fakeExtra{err: tt.storeErr, group: store.Group{ID: groupUUID, Name: "g"}}}
			h, logs := newTestAPI(t, fs)
			rec, resp := do(t, h, tt.method, tt.path, "", tt.body, asSession)
			if tt.wantCode != "" {
				wantError(t, rec, resp, tt.wantStatus, tt.wantCode)
			} else if rec.Code != tt.wantStatus {
				t.Fatalf("status got %d, want %d (%s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus == 500 {
				if !strings.Contains(logs.String(), "boom") || strings.Contains(rec.Body.String(), "boom") {
					t.Errorf("el 500 debe loguear el error real y no filtrarlo")
				}
			}
			if tt.wantStatus < 300 {
				want := map[string]string{"POST": "group_created", "PATCH": "group_updated", "DELETE": "group_deleted"}[tt.method]
				if ev := auditEvents(fs); !slices.Equal(ev, []string{want}) {
					t.Errorf("auditoria got %v, want [%s]", ev, want)
				}
			} else if len(auditEvents(fs)) != 0 {
				t.Errorf("una escritura fallida no se audita como exito: %v", auditEvents(fs))
			}
		})
	}
}

func TestGroupPatchSemantics(t *testing.T) {
	t.Parallel()
	fs := &fakeStore{x: fakeExtra{group: store.Group{ID: groupUUID, Name: "g"}}}
	h, _ := newTestAPI(t, fs)
	// null explicito = volver al umbral global; ausente = no tocar.
	do(t, h, "PATCH", "/api/v1/groups/"+groupUUID, "", `{"down_after_seconds":null}`, asSession)
	if c := fs.x.gotChange; !c.DownAfterSet || c.DownAfterSeconds != nil || c.Name != nil || c.AlwaysOn != nil {
		t.Errorf("null explicito got %+v", c)
	}
	do(t, h, "PATCH", "/api/v1/groups/"+groupUUID, "", `{"always_on":true}`, asSession)
	if c := fs.x.gotChange; c.DownAfterSet || c.AlwaysOn == nil || !*c.AlwaysOn {
		t.Errorf("ausente got %+v", c)
	}
	do(t, h, "PATCH", "/api/v1/groups/"+groupUUID, "", `{"name":"  Recortado  "}`, asSession)
	if c := fs.x.gotChange; c.Name == nil || *c.Name != "Recortado" {
		t.Errorf("nombre recortado got %+v", c)
	}
}

func TestGroupAuditUsesWhitelistedFieldsOnly(t *testing.T) {
	t.Parallel()
	fs := &fakeStore{x: fakeExtra{group: store.Group{ID: groupUUID, Name: "Laptops", AlwaysOn: true}}}
	h, _ := newTestAPI(t, fs)
	do(t, h, "POST", "/api/v1/groups", "", `{"name":"Laptops","always_on":true}`, asSession)
	if len(fs.audits) != 1 {
		t.Fatalf("auditorias got %d", len(fs.audits))
	}
	var detail map[string]any
	if err := json.Unmarshal(fs.audits[0].Detail, &detail); err != nil {
		t.Fatal(err)
	}
	for k := range detail {
		if !auditDetailKeys[k] {
			t.Errorf("clave %q fuera de la lista blanca", k)
		}
	}
	if detail["name"] != "Laptops" || detail["always_on"] != true || fs.audits[0].TargetID != groupUUID || fs.audits[0].ActorUsername != "admin" {
		t.Errorf("detalle got %v / %+v", detail, fs.audits[0])
	}
}

func TestListGroupsShape(t *testing.T) {
	t.Parallel()
	down := 300
	fs := &fakeStore{x: fakeExtra{
		groups:    []store.Group{{ID: groupUUID, Name: "Laptops", DownAfterSeconds: &down, AlwaysOn: false, HostCount: 3}, {ID: "g2", Name: "Srv", AlwaysOn: true}},
		ungrouped: 4,
	}}
	viewerStore(fs)
	h := New(fs, testHasher(), slog.New(slog.NewJSONHandler(io.Discard, nil)), func() time.Time { return fixedNow }, Options{HostDownAfter: 2 * time.Minute}).Routes()
	rec, resp := do(t, h, "GET", "/api/v1/groups", "", "", asSession)
	if rec.Code != 200 {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}
	var data struct {
		Items []map[string]any `json:"items"`
		Ung   map[string]any   `json:"ungrouped"`
		Def   map[string]any   `json:"defaults"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		t.Fatal(err)
	}
	wantKeys(t, "grupo", data.Items[0], "id", "name", "down_after_seconds", "always_on", "host_count", "detail_retention_days", "effective_detail_days", "retention_source")
	if data.Items[0]["down_after_seconds"] != 300.0 || data.Items[1]["down_after_seconds"] != nil || data.Items[0]["host_count"] != 3.0 || data.Items[1]["always_on"] != true {
		t.Errorf("items got %v", data.Items)
	}
	if data.Ung["host_count"] != 4.0 || data.Def["down_after_seconds"] != 120.0 {
		t.Errorf("ungrouped/defaults got %v / %v", data.Ung, data.Def)
	}
}

func TestSetHostGroup(t *testing.T) {
	t.Parallel()
	path := "/api/v1/hosts/" + hostUUID + "/group"
	tests := []struct {
		name       string
		path       string
		body       string
		storeErr   error
		wantStatus int
		wantCode   string
		wantGroup  *string
	}{
		{"asignar", path, `{"group_id":"` + groupUUID + `"}`, nil, 200, "", strPtr(groupUUID)},
		{"quitar con null", path, `{"group_id":null}`, nil, 200, "", nil},
		{"group_id ausente", path, `{}`, nil, 400, "invalid_body", nil},
		{"group_id invalido", path, `{"group_id":"x"}`, nil, 400, "invalid_body", nil},
		{"campo desconocido", path, `{"group_id":null,"x":1}`, nil, 400, "invalid_body", nil},
		{"host id invalido", "/api/v1/hosts/x/group", `{"group_id":null}`, nil, 400, "invalid_id", nil},
		{"host inexistente", path, `{"group_id":null}`, store.ErrNotFound, 404, "not_found", nil},
		{"grupo inexistente", path, `{"group_id":"` + groupUUID + `"}`, store.ErrGroupNotFound, 422, "group_not_found", nil},
		{"error de db", path, `{"group_id":null}`, errors.New("boom"), 500, "internal_error", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := &fakeStore{x: fakeExtra{err: tt.storeErr, hostGroup: store.HostGroupResult{Group: &store.GroupRef{ID: groupUUID, Name: "g"}, Status: "offline"}}}
			h, _ := newTestAPI(t, fs)
			rec, resp := do(t, h, "PUT", tt.path, "", tt.body, asSession)
			if tt.wantCode != "" {
				wantError(t, rec, resp, tt.wantStatus, tt.wantCode)
				return
			}
			if rec.Code != 200 {
				t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
			}
			if (fs.x.gotGroup == nil) != (tt.wantGroup == nil) || (tt.wantGroup != nil && *fs.x.gotGroup != *tt.wantGroup) {
				t.Errorf("group_id al store got %v, want %v", fs.x.gotGroup, tt.wantGroup)
			}
			data := dataMap(t, resp)
			wantKeys(t, "respuesta", data, "group", "status")
			if data["status"] != "offline" {
				t.Errorf("status got %v", data["status"])
			}
			if ev := auditEvents(fs); !slices.Equal(ev, []string{"host_group_set"}) {
				t.Errorf("auditoria got %v", ev)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

func TestBulkHosts(t *testing.T) {
	t.Parallel()
	ids := func(n int) string {
		parts := make([]string, n)
		for i := range n {
			parts[i] = fmt.Sprintf(`"00000000-0000-4000-8000-%012d"`, i)
		}
		return strings.Join(parts, ",")
	}
	one := `"` + hostUUID + `"`
	tests := []struct {
		name       string
		body       string
		storeErr   error
		wantStatus int
		wantCode   string
		check      func(t *testing.T, b store.Bulk)
	}{
		{"set_group", `{"ids":[` + one + `],"action":"set_group","group_id":"` + groupUUID + `"}`, nil, 200, "", func(t *testing.T, b store.Bulk) {
			if b.GroupID == nil || *b.GroupID != groupUUID || b.Action != store.BulkSetGroup {
				t.Errorf("bulk got %+v", b)
			}
		}},
		{"set_group null = sin grupo", `{"ids":[` + one + `],"action":"set_group","group_id":null}`, nil, 200, "", func(t *testing.T, b store.Bulk) {
			if b.GroupID != nil {
				t.Errorf("GroupID got %v, want nil", b.GroupID)
			}
		}},
		{"set_group sin group_id", `{"ids":[` + one + `],"action":"set_group"}`, nil, 400, "invalid_body", nil},
		{"tag ya no existe en el body", `{"ids":[` + one + `],"action":"set_group","group_id":null,"tag":"a"}`, nil, 400, "invalid_body", nil},
		{"add_tag ya no existe", `{"ids":[` + one + `],"action":"add_tag","tag":"x"}`, nil, 400, "invalid_body", nil},
		{"remove_tag ya no existe", `{"ids":[` + one + `],"action":"remove_tag","tag":"x"}`, nil, 400, "invalid_body", nil},
		{"disable con group_id", `{"ids":[` + one + `],"action":"disable","group_id":null}`, nil, 400, "invalid_body", nil},
		{"disable", `{"ids":[` + one + `],"action":"disable"}`, nil, 200, "", nil},
		{"disable con extras", `{"ids":[` + one + `],"action":"disable","tag":"x"}`, nil, 400, "invalid_body", nil},
		{"accion desconocida", `{"ids":[` + one + `],"action":"borrar"}`, nil, 400, "invalid_body", nil},
		{"sin accion", `{"ids":[` + one + `]}`, nil, 400, "invalid_body", nil},
		{"sin ids", `{"ids":[],"action":"disable"}`, nil, 400, "invalid_body", nil},
		{"200 ids entran", `{"ids":[` + ids(200) + `],"action":"disable"}`, nil, 200, "", func(t *testing.T, b store.Bulk) {
			if len(b.IDs) != 200 {
				t.Errorf("ids got %d", len(b.IDs))
			}
		}},
		{"201 ids", `{"ids":[` + ids(201) + `],"action":"disable"}`, nil, 400, "invalid_body", nil},
		{"id invalido", `{"ids":["x"],"action":"disable"}`, nil, 400, "invalid_body", nil},
		{"ids repetidos se deduplican", `{"ids":[` + one + `,` + one + `],"action":"disable"}`, nil, 200, "", func(t *testing.T, b store.Bulk) {
			if len(b.IDs) != 1 {
				t.Errorf("ids got %v, want 1", b.IDs)
			}
		}},
		{"campo desconocido", `{"ids":[` + one + `],"action":"disable","x":1}`, nil, 400, "invalid_body", nil},
		{"host inexistente", `{"ids":[` + one + `],"action":"disable"}`, store.ErrUnknownHosts, 422, "unknown_hosts", nil},
		{"grupo inexistente", `{"ids":[` + one + `],"action":"set_group","group_id":"` + groupUUID + `"}`, store.ErrGroupNotFound, 422, "group_not_found", nil},
		{"error de db", `{"ids":[` + one + `],"action":"disable"}`, errors.New("boom"), 500, "internal_error", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := &fakeStore{x: fakeExtra{err: tt.storeErr, affected: 7}}
			h, _ := newTestAPI(t, fs)
			rec, resp := do(t, h, "POST", "/api/v1/hosts/bulk", "", tt.body, asSession)
			if tt.wantCode != "" {
				wantError(t, rec, resp, tt.wantStatus, tt.wantCode)
				return
			}
			if rec.Code != 200 {
				t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
			}
			if got := dataMap(t, resp)["affected"]; got != 7.0 {
				t.Errorf("affected got %v, want 7", got)
			}
			if tt.check != nil {
				tt.check(t, fs.x.gotBulk)
			}
			if len(fs.audits) != 1 || fs.audits[0].Event != "hosts_bulk" {
				t.Errorf("auditoria got %+v", fs.audits)
			}
		})
	}
}

func TestEnrollTokenWithGroup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		body       string
		storeErr   error
		wantStatus int
		wantCode   string
		wantGroup  *string
	}{
		{"sin grupo", `{"hostname":"a"}`, nil, 201, "", nil},
		{"con grupo", `{"hostname":"a","group_id":"` + groupUUID + `"}`, nil, 201, "", strPtr(groupUUID)},
		{"group_id null", `{"hostname":"a","group_id":null}`, nil, 201, "", nil},
		{"group_id invalido", `{"hostname":"a","group_id":"x"}`, nil, 400, "invalid_body", nil},
		{"grupo inexistente", `{"hostname":"a","group_id":"` + groupUUID + `"}`, store.ErrGroupNotFound, 422, "group_not_found", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := &fakeStore{createErr: tt.storeErr}
			h, _ := newTestAPI(t, fs)
			rec, resp := do(t, h, "POST", "/api/v1/hosts/enroll-token", "", tt.body, asSession)
			if tt.wantCode != "" {
				wantError(t, rec, resp, tt.wantStatus, tt.wantCode)
				return
			}
			if rec.Code != 201 {
				t.Fatalf("status %d", rec.Code)
			}
			// La respuesta no cambia: solo enroll_token y expires_at.
			wantKeys(t, "respuesta", dataMap(t, resp), "enroll_token", "expires_at")
			if (fs.enrollGroup == nil) != (tt.wantGroup == nil) || (tt.wantGroup != nil && *fs.enrollGroup != *tt.wantGroup) {
				t.Errorf("group_id al store got %v, want %v", fs.enrollGroup, tt.wantGroup)
			}
		})
	}
}

// Lectura: viewer y admin; escritura: solo admin (403 con viewer, 401 sin sesion).
func TestNewRoutesRoles(t *testing.T) {
	t.Parallel()
	reads := []string{"/api/v1/groups", "/api/v1/summary", "/api/v1/hosts"}
	for _, p := range reads {
		t.Run("lectura "+p, func(t *testing.T) {
			t.Parallel()
			h, _ := newTestAPI(t, viewerStore(&fakeStore{}))
			if rec, _ := do(t, h, "GET", p, "", "", asSession); rec.Code != 200 {
				t.Errorf("viewer got %d, want 200", rec.Code)
			}
			rec, resp := do(t, h, "GET", p, "", "")
			wantError(t, rec, resp, 401, "unauthenticated")
		})
	}
	writes := []struct{ method, path, body string }{
		{"POST", "/api/v1/groups", `{"name":"x"}`},
		{"PATCH", "/api/v1/groups/" + groupUUID, `{"name":"x"}`},
		{"DELETE", "/api/v1/groups/" + groupUUID, ``},
		{"PUT", "/api/v1/hosts/" + hostUUID + "/group", `{"group_id":null}`},
		{"POST", "/api/v1/hosts/bulk", `{"ids":["` + hostUUID + `"],"action":"disable"}`},
	}
	for _, w := range writes {
		t.Run("escritura "+w.method+" "+w.path, func(t *testing.T) {
			t.Parallel()
			fs := viewerStore(&fakeStore{})
			h, _ := newTestAPI(t, fs)
			rec, resp := do(t, h, w.method, w.path, "", w.body, asSession)
			wantError(t, rec, resp, http.StatusForbidden, "forbidden")
			rec, resp = do(t, h, w.method, w.path, "", w.body)
			wantError(t, rec, resp, http.StatusUnauthorized, "unauthenticated")
			if fs.x.gotBulk.Action != "" || fs.x.deleted != "" {
				t.Errorf("el store no debe recibir nada de un viewer / sin sesion")
			}
		})
	}
}

// Las rutas de etiquetas ya no existen (404), no quedan como rutas medio vivas.
func TestTagRoutesRemoved(t *testing.T) {
	t.Parallel()
	routes := []struct{ method, path, body string }{
		{"GET", "/api/v1/tags", ""},
		{"PUT", "/api/v1/hosts/" + hostUUID + "/tags", `{"tags":[]}`},
	}
	for _, r := range routes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			t.Parallel()
			h, _ := newTestAPI(t, &fakeStore{})
			req := httptest.NewRequest(r.method, r.path, strings.NewReader(r.body))
			req.Host = "localhost:8080"
			req.Header.Set("Content-Type", "application/json")
			asSession(req)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("got %d, want 404 (ruta eliminada)", rec.Code)
			}
		})
	}
}
