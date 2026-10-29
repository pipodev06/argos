package sender

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "super-secreto"

func newClient(t *testing.T, endpoint string, mutate func(*Options)) (*Client, *[]time.Duration) {
	t.Helper()
	var sleeps []time.Duration
	o := Options{
		Endpoint:    endpoint,
		Token:       testToken,
		MaxAttempts: 3,
		BaseDelay:   time.Second,
		MaxDelay:    8 * time.Second,
		Sleep: func(_ context.Context, d time.Duration) error {
			sleeps = append(sleeps, d)
			return nil
		},
	}
	if mutate != nil {
		mutate(&o)
	}
	if o.HTTPClient == nil {
		// Transporte propio por cliente: httptest.Server.Close() llama a CloseIdleConnections del DefaultTransport y, con
		// tests en paralelo, cortaba a mitad la conexion de otro test (flaky).
		o.HTTPClient = &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}
	}
	c, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return c, &sleeps
}

func report(ts int64) Report {
	return Report{Hostname: "h", AgentVersion: "test", Timestamp: ts, Metrics: Metrics{CPUPct: 1}}
}

func TestSendOK(t *testing.T) {
	t.Parallel()

	var gotAuth, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotAuth, gotPath, gotBody = r.Header.Get("Authorization"), r.URL.Path, string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newClient(t, srv.URL, nil)
	if err := c.Send(context.Background(), report(1)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotPath != "/api/v1/agent/metrics" {
		t.Errorf("path got %q", gotPath)
	}
	if gotAuth != "Bearer "+testToken {
		t.Errorf("Authorization got %q", gotAuth)
	}
	if strings.Contains(gotBody, testToken) || strings.Contains(gotBody, "agent_id") {
		t.Errorf("el body no debe llevar el token ni agent_id: %s", gotBody)
	}
	var back Report
	if err := json.Unmarshal([]byte(gotBody), &back); err != nil || back.Timestamp != 1 {
		t.Errorf("body invalido: %v %s", err, gotBody)
	}
	if c.Buffered() != 0 {
		t.Errorf("Buffered got %d, want 0", c.Buffered())
	}
}

func TestSendStatusHandling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		status       int
		wantRequests int32
		wantBuffered int
		wantStatus   int
	}{
		{name: "400 no se reintenta", status: 400, wantRequests: 1, wantBuffered: 0, wantStatus: 400},
		{name: "500 se reintenta y queda en buffer", status: 500, wantRequests: 3, wantBuffered: 1, wantStatus: 500},
		{name: "503 se reintenta", status: 503, wantRequests: 3, wantBuffered: 1, wantStatus: 503},
		{name: "429 se reintenta", status: 429, wantRequests: 3, wantBuffered: 1, wantStatus: 429},
		{name: "408 se reintenta", status: 408, wantRequests: 3, wantBuffered: 1, wantStatus: 408},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var n atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n.Add(1)
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			c, sleeps := newClient(t, srv.URL, nil)
			err := c.Send(context.Background(), report(1))
			var se *StatusError
			if !errors.As(err, &se) || se.Code != tt.wantStatus {
				t.Fatalf("err got %v, want StatusError %d", err, tt.wantStatus)
			}
			if n.Load() != tt.wantRequests {
				t.Errorf("requests got %d, want %d", n.Load(), tt.wantRequests)
			}
			if c.Buffered() != tt.wantBuffered {
				t.Errorf("Buffered got %d, want %d", c.Buffered(), tt.wantBuffered)
			}
			if want := int(tt.wantRequests) - 1; len(*sleeps) != want {
				t.Errorf("esperas got %d, want %d", len(*sleeps), want)
			}
		})
	}
}

func TestSendRetriesUntilSuccess(t *testing.T) {
	t.Parallel()

	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newClient(t, srv.URL, nil)
	if err := c.Send(context.Background(), report(1)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if n.Load() != 3 || c.Buffered() != 0 {
		t.Errorf("requests=%d buffered=%d, want 3 y 0", n.Load(), c.Buffered())
	}
}

func TestBackoffIsExponentialWithJitterAndCapped(t *testing.T) {
	t.Parallel()

	c, _ := newClient(t, "http://x", nil) // base 1s, max 8s
	for attempt, ceil := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second, 8 * time.Second} {
		for range 50 {
			d := c.backoff(attempt)
			if d < ceil/2 || d > ceil {
				t.Fatalf("backoff(%d) got %v, want en [%v, %v]", attempt, d, ceil/2, ceil)
			}
		}
	}
}

// roundTripFunc simula la red sin abrir sockets.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNetworkDownBuffersAndDrainsInOrder(t *testing.T) {
	t.Parallel()

	var down atomic.Bool
	down.Store(true)
	var delivered []int64
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if down.Load() {
			return nil, errors.New("connection refused")
		}
		var rep Report
		if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
			return nil, err
		}
		delivered = append(delivered, rep.Timestamp)
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: http.Header{}}, nil
	})
	c, _ := newClient(t, "http://argos.invalid", func(o *Options) { o.HTTPClient = &http.Client{Transport: rt} })

	for ts := int64(1); ts <= 3; ts++ {
		if err := c.Send(context.Background(), report(ts)); err == nil {
			t.Fatalf("Send %d: se esperaba error con la red caida", ts)
		}
	}
	if c.Buffered() != 3 {
		t.Fatalf("Buffered got %d, want 3", c.Buffered())
	}

	down.Store(false)
	if err := c.Send(context.Background(), report(4)); err != nil {
		t.Fatalf("Send tras recuperar la red: %v", err)
	}
	want := []int64{4, 1, 2, 3} // el vivo primero, luego el historico oldest-first
	if len(delivered) != len(want) {
		t.Fatalf("delivered got %v, want %v", delivered, want)
	}
	for i := range want {
		if delivered[i] != want[i] {
			t.Fatalf("delivered got %v, want %v (orden)", delivered, want)
		}
	}
	if c.Buffered() != 0 {
		t.Errorf("Buffered got %d, want 0", c.Buffered())
	}
}

func TestBufferIsBoundedDroppingOldest(t *testing.T) {
	t.Parallel()

	var got []int64
	var down atomic.Bool
	down.Store(true)
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if down.Load() {
			return nil, errors.New("sin red")
		}
		var rep Report
		if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
			return nil, err
		}
		got = append(got, rep.Timestamp)
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: http.Header{}}, nil
	})
	c, _ := newClient(t, "http://argos.invalid", func(o *Options) {
		o.MaxBuffer = 3
		o.HTTPClient = &http.Client{Transport: rt}
	})

	for ts := int64(1); ts <= 5; ts++ {
		_ = c.Send(context.Background(), report(ts)) // falla a proposito: red caida
	}
	if c.Buffered() != 3 {
		t.Fatalf("Buffered got %d, want 3 (tope)", c.Buffered())
	}
	down.Store(false)
	if err := c.Send(context.Background(), report(6)); err != nil {
		t.Fatal(err)
	}
	want := []int64{6, 3, 4, 5} // el vivo primero; el tope descarto 1 y 2
	if !slices.Equal(got, want) {
		t.Errorf("entregados got %v, want %v (los mas viejos se descartan)", got, want)
	}
}

func TestSendStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, _ := newClient(t, srv.URL, func(o *Options) {
		o.Sleep = func(ctx context.Context, _ time.Duration) error {
			cancel()
			return ctx.Err()
		}
	})
	if err := c.Send(ctx, report(1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("err got %v, want context.Canceled", err)
	}
	if n.Load() != 1 {
		t.Errorf("requests got %d, want 1 (no reintenta tras cancelar)", n.Load())
	}
	if c.Buffered() != 1 {
		t.Errorf("Buffered got %d, want 1", c.Buffered())
	}
}
