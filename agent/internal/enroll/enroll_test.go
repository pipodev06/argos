package enroll

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const enrollToken = "enroll-secreto-abc"

func TestDoHappyPath(t *testing.T) {
	t.Parallel()

	var gotPath, gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"host_id":"11111111-2222-3333-4444-555555555555","agent_token":"agent-tok"},"error":null}`))
	}))
	defer srv.Close()

	res, err := Do(context.Background(), srv.Client(), srv.URL, enrollToken, "linux", "amd64", "mi-host")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if res.HostID != "11111111-2222-3333-4444-555555555555" || res.AgentToken != "agent-tok" {
		t.Errorf("res got %+v", res)
	}
	if gotPath != "/api/v1/agent/enroll" || gotAuth != "" {
		t.Errorf("path=%q auth=%q, want el endpoint de enroll y sin Authorization", gotPath, gotAuth)
	}
	if gotBody["enroll_token"] != enrollToken || gotBody["os"] != "linux" || gotBody["arch"] != "amd64" ||
		gotBody["hostname"] != "mi-host" || len(gotBody) != 4 {
		t.Errorf("body got %v", gotBody)
	}
}

func TestDoErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		header     map[string]string
		body       string
		wantIs     error
		wantSubstr string
	}{
		{name: "token invalido", status: 401, body: `{"data":null,"error":{"code":"invalid_token","message":"x"}}`, wantIs: ErrInvalidToken},
		{name: "rate limit", status: 429, header: map[string]string{"Retry-After": "12"}, wantIs: ErrRateLimited, wantSubstr: "12s"},
		{name: "error del servidor con codigo", status: 500, body: `{"error":{"code":"internal_error"}}`, wantSubstr: "500 internal_error"},
		{name: "payload grande", status: 413, body: `{"error":{"code":"payload_too_large"}}`, wantSubstr: "413"},
		{name: "201 sin datos", status: 201, body: `{"data":null}`, wantSubstr: "incompleta"},
		{name: "201 ilegible", status: 201, body: `no-json`, wantSubstr: "ilegible"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tt.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			_, err := Do(context.Background(), srv.Client(), srv.URL, enrollToken, "linux", "amd64", "mi-host")
			if err == nil {
				t.Fatal("se esperaba error")
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("err got %v, want %v", err, tt.wantIs)
			}
			if tt.wantSubstr != "" && !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Errorf("err got %q, want que contenga %q", err, tt.wantSubstr)
			}
			if strings.Contains(err.Error(), enrollToken) {
				t.Errorf("el error revela el enroll token: %v", err)
			}
		})
	}
}

func TestDoNetworkErrorDoesNotLeakToken(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // puerto cerrado: connection refused

	_, err := Do(context.Background(), http.DefaultClient, url, enrollToken, "linux", "amd64", "mi-host")
	if err == nil {
		t.Fatal("se esperaba error de red")
	}
	if strings.Contains(err.Error(), enrollToken) {
		t.Errorf("el error revela el enroll token: %v", err)
	}
}
