package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"argos/server/internal/store"
)

// API de la regla exposed_port: validacion de expected_ports y detalle de la alerta segun can_inventory.

func TestExposedPortRuleValidation(t *testing.T) {
	e := newAlertEnv(t)
	const head = `{"name":"x","kind":"exposed_port","duration_seconds":0,"severity":"warn"`
	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf(`{"proto":"tcp","port":%d}`, i+1)
	}
	bad := map[string]string{
		"proto invalido":      head + `,"expected_ports":[{"proto":"icmp","port":80}]}`,
		"puerto cero":         head + `,"expected_ports":[{"proto":"tcp","port":0}]}`,
		"puerto enorme":       head + `,"expected_ports":[{"proto":"tcp","port":65536}]}`,
		"repetido":            head + `,"expected_ports":[{"proto":"tcp","port":80},{"proto":"tcp","port":80}]}`,
		"campo extra":         head + `,"expected_ports":[{"proto":"tcp","port":80,"bind":"x"}]}`,
		"mas de 100":          head + `,"expected_ports":[` + strings.Join(tooMany, ",") + `]}`,
		"con umbral":          head + `,"threshold":5}`,
		"con metric":          head + `,"metric":"cpu_pct"}`,
		"expected en metrica": `{"name":"m","kind":"metric","metric":"cpu_pct","op":"gt","threshold":90,"duration_seconds":0,"severity":"warn","expected_ports":[{"proto":"tcp","port":80}]}`,
	}
	for name, body := range bad {
		if rec := e.do("POST", "/api/v1/alert-rules", body, withCookie(e.admin)); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status got %d, want 400 (%s)", name, rec.Code, rec.Body.String())
		}
	}

	// 100 puertos esperados (con espacios) caben en el limite de body de las reglas.
	pretty := make([]string, 100)
	for i := range pretty {
		pretty[i] = fmt.Sprintf(`{ "proto": "tcp", "port": %d }`, i+1)
	}
	id := e.createRule(`{"name":"cien","kind":"exposed_port","duration_seconds":0,"severity":"warn","expected_ports":[` + strings.Join(pretty, ", ") + `]}`)

	// PATCH: reemplaza la lista completa; kind sigue inmutable.
	rec := e.do("PATCH", "/api/v1/alert-rules/"+id, `{"expected_ports":[{"proto":"udp","port":53}]}`, withCookie(e.admin))
	e.wantStatus(rec, http.StatusOK, "")
	var got struct {
		Rule struct {
			Kind          string `json:"kind"`
			Severity      string `json:"severity"`
			ExpectedPorts []struct {
				Proto string `json:"proto"`
				Port  int    `json:"port"`
			} `json:"expected_ports"`
		} `json:"rule"`
	}
	e.dataAs(rec, &got)
	if got.Rule.Kind != "exposed_port" || len(got.Rule.ExpectedPorts) != 1 || got.Rule.ExpectedPorts[0].Proto != "udp" || got.Rule.ExpectedPorts[0].Port != 53 {
		t.Errorf("regla got %+v", got.Rule)
	}
	e.wantStatus(e.do("PATCH", "/api/v1/alert-rules/"+id, `{"expected_ports":[{"proto":"x","port":1}]}`, withCookie(e.admin)), http.StatusBadRequest, "")
	// Una regla de otro kind devuelve expected_ports como arreglo vacio.
	mid := e.createRule(cpuRuleBody)
	rec = e.do("GET", "/api/v1/alert-rules/"+mid, "", withCookie(e.viewer))
	if !strings.Contains(rec.Body.String(), `"expected_ports":[]`) {
		t.Errorf("expected_ports de una regla metric debe ser [], body %s", rec.Body.String())
	}
}

func TestExposedPortAlertDetailNeedsInventoryPermission(t *testing.T) {
	e := newAlertEnv(t)
	e.newUser("invuser", pwViewer, store.RoleViewer, false)
	mustExec(t, `UPDATE users SET can_inventory = true WHERE username = 'invuser'`)
	invCookie := e.login("invuser", pwViewer)

	hostID, _, _ := e.enrollHost(e.admin, "srv")
	e.createRule(`{"name":"puertos","kind":"exposed_port","duration_seconds":0,"resolve_seconds":0,"severity":"warn"}`)
	ingest := func(items ...string) {
		raw := make([]json.RawMessage, len(items))
		for i, it := range items {
			raw[i] = json.RawMessage(it)
		}
		sum := sha256.Sum256([]byte(strings.Join(items, ",")))
		in := store.InventoryFull{Module: store.InventoryPorts, Rev: hex.EncodeToString(sum[:]), Items: raw, CollectedAt: time.Now()}
		if err := e.st.IngestInventory(context.Background(), hostID, in, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	ingest()
	ingest(`{"proto":"tcp","bind":"0.0.0.0","port":4444}`)
	if r := e.eval(); r.Opened != 1 {
		t.Fatalf("opened got %d, want 1", r.Opened)
	}

	detailFor := func(c *http.Cookie) (detail *string, trigger *float64) {
		rec := e.do("GET", "/api/v1/alerts", "", withCookie(c))
		e.wantStatus(rec, http.StatusOK, "")
		var p struct {
			Items []struct {
				Kind         string   `json:"kind"`
				Detail       *string  `json:"detail"`
				TriggerValue *float64 `json:"trigger_value"`
			} `json:"items"`
		}
		e.dataAs(rec, &p)
		if len(p.Items) != 1 || p.Items[0].Kind != "exposed_port" {
			t.Fatalf("alertas got %+v", p.Items)
		}
		return p.Items[0].Detail, p.Items[0].TriggerValue
	}
	if d, tv := detailFor(e.viewer); d != nil || tv == nil || *tv != 1 {
		t.Errorf("viewer sin can_inventory: detail got %v (want null), trigger %v (want 1)", d, tv)
	}
	for name, c := range map[string]*http.Cookie{"admin": e.admin, "con can_inventory": invCookie} {
		if d, _ := detailFor(c); d == nil || *d != "tcp/4444" {
			t.Errorf("%s: detail got %v, want tcp/4444", name, d)
		}
	}
}
