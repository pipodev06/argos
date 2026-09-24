package sender

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"argos/agent/internal/checks"
)

func TestSendAppliesChecksFromResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		body      string
		wantCalls int
		wantRev   string
	}{
		{"server nuevo con lista", `{"data":{"checks":{"rev":"abc123","items":[{"id":"1","type":"tcp","name":"n","interval_s":60,"timeout_ms":1000,"retries":1,"target_host":"127.0.0.1","target_port":22,"campo_futuro":true}]}}}`, 1, "abc123"},
		{"server nuevo sin cambios (omite checks)", `{"data":{"ok":true}}`, 0, ""},
		{"server viejo", `{"ok":true}`, 0, ""},
		{"cuerpo vacio", ``, 0, ""},
		{"cuerpo ilegible", `<html>`, 0, ""},
		{"checks null", `{"data":{"checks":null}}`, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()
			var got []checks.Set
			c, _ := newClient(t, srv.URL, func(o *Options) {
				o.OnChecks = func(_ context.Context, s checks.Set) { got = append(got, s) }
			})
			if err := c.Send(t.Context(), report(1)); err != nil {
				t.Fatalf("Send: %v", err)
			}
			if len(got) != tt.wantCalls {
				t.Fatalf("OnChecks llamado %d veces, want %d", len(got), tt.wantCalls)
			}
			if tt.wantCalls == 1 && (got[0].Rev != tt.wantRev || len(got[0].Items) != 1 || got[0].Items[0].TargetPort != 22) {
				t.Errorf("lista mal decodificada: %+v", got[0])
			}
		})
	}
}

func TestSendWithoutOnChecksIgnoresList(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"checks":{"rev":"x","items":[]}}}`)
	}))
	defer srv.Close()
	c, _ := newClient(t, srv.URL, nil)
	if err := c.Send(t.Context(), report(1)); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestReportCarriesChecksRevAndServices(t *testing.T) {
	t.Parallel()
	var body, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body, auth = string(b), r.Header.Get("Authorization")
	}))
	defer srv.Close()
	c, _ := newClient(t, srv.URL, nil)
	v := 12.5
	r := report(1)
	r.ChecksRev = "0123456789abcdef"
	r.Services = []checks.Result{
		{CheckID: "u1", Name: "web", Status: "up", ResponseMS: 12, Detail: "ok", TS: 5},
		{CheckID: "u2", Name: "cert", Status: "warn", Value: &v, Detail: "ok", TS: 6},
	}
	if err := c.Send(t.Context(), r); err != nil {
		t.Fatalf("Send: %v", err)
	}
	var got struct {
		ChecksRev string           `json:"checks_rev"`
		Services  []map[string]any `json:"services"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body: %v", err)
	}
	if got.ChecksRev != "0123456789abcdef" || len(got.Services) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got.Services[0]["check_id"] != "u1" || got.Services[0]["value"] != nil || got.Services[1]["value"] != 12.5 {
		t.Errorf("servicios mal serializados: %v", got.Services)
	}
	if strings.Contains(body, testToken) || auth != "Bearer "+testToken {
		t.Errorf("el token no debe ir en el body y si en Authorization (auth=%q)", auth)
	}

	// Sin chequeos, ambos campos se omiten (compatible con un server viejo).
	body = ""
	if err := c.Send(t.Context(), report(2)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "checks_rev") || strings.Contains(body, "services") {
		t.Errorf("campos de chequeos presentes sin datos: %s", body)
	}
}
