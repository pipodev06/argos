package sender

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSendBodyCarriesNetCounters(t *testing.T) {
	t.Parallel()

	var raw map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&raw)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := newClient(t, srv.URL, nil)
	r := report(1)
	r.Metrics = Metrics{CPUPct: 12.5, MemUsedMB: 100, MemTotalMB: 200, Net: NetMetrics{RxBytes: 7, TxBytes: 9}}
	if err := c.Send(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	// Forma exacta que valida el server: metrics.net.{rx_bytes,tx_bytes}.
	m, _ := raw["metrics"].(map[string]any)
	n, _ := m["net"].(map[string]any)
	if n["rx_bytes"] != float64(7) || n["tx_bytes"] != float64(9) || m["cpu_pct"] != 12.5 {
		t.Errorf("metrics got %v", m)
	}
}

func TestSend401StopsWithoutRetryOrDrop(t *testing.T) {
	t.Parallel()

	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c, sleeps := newClient(t, srv.URL, nil)
	err := c.Send(context.Background(), report(1))
	if !errors.Is(err, ErrTokenRejected) {
		t.Fatalf("err got %v, want ErrTokenRejected", err)
	}
	if n.Load() != 1 || len(*sleeps) != 0 {
		t.Errorf("requests=%d esperas=%d, want 1 y 0 (un 401 no se reintenta)", n.Load(), len(*sleeps))
	}
	if c.Buffered() != 1 {
		t.Errorf("Buffered got %d, want 1 (no se descarta en silencio)", c.Buffered())
	}
}

func TestSend429HonorsRetryAfter(t *testing.T) {
	t.Parallel()

	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, sleeps := newClient(t, srv.URL, nil)
	if err := c.Send(context.Background(), report(1)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(*sleeps) != 1 || (*sleeps)[0] != 7*time.Second {
		t.Errorf("esperas got %v, want [7s] (Retry-After manda sobre el backoff)", *sleeps)
	}
	if n.Load() != 2 || c.Buffered() != 0 {
		t.Errorf("requests=%d buffered=%d, want 2 y 0", n.Load(), c.Buffered())
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"3", 3 * time.Second},
		{"0", 0},
		{"-5", 0},
		{"abc", 0},
		{"99999999", maxRetryAfter},
		{now.Add(10 * time.Second).Format(http.TimeFormat), 10 * time.Second},
		{now.Add(-time.Hour).Format(http.TimeFormat), 0},
	}
	for _, tt := range tests {
		if got := parseRetryAfter(tt.in, now); got != tt.want {
			t.Errorf("parseRetryAfter(%q) got %v, want %v", tt.in, got, tt.want)
		}
	}
}

// flakyNet simula una red que se puede caer y cuenta lo entregado.
type flakyNet struct {
	down      atomic.Bool
	delivered atomic.Int32
}

func (f *flakyNet) client() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		if f.down.Load() {
			return nil, errors.New("sin red")
		}
		f.delivered.Add(1)
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: http.Header{}}, nil
	})}
}

func TestDrainIsPacedBelowServerLimit(t *testing.T) {
	t.Parallel()

	net := &flakyNet{}
	net.down.Store(true)
	c, sleeps := newClient(t, "http://argos.invalid", func(o *Options) { o.HTTPClient = net.client() })
	for ts := int64(1); ts <= 4; ts++ {
		_ = c.Send(context.Background(), report(ts)) // red caida a proposito
	}

	net.down.Store(false)
	*sleeps = nil
	if err := c.Send(context.Background(), report(5)); err != nil {
		t.Fatal(err)
	}
	if net.delivered.Load() != 5 {
		t.Fatalf("delivered got %d, want 5", net.delivered.Load())
	}
	if len(*sleeps) != 4 {
		t.Fatalf("pausas got %v, want 4 (entre 5 envios)", *sleeps)
	}
	for _, d := range *sleeps {
		// El server permite 1 req cada 2 s: la pausa no puede ser menor.
		if d < 2*time.Second {
			t.Errorf("pausa %v menor que el limite del server (2s)", d)
		}
	}
}

func TestSendCapsSendsPerCallToTheInterval(t *testing.T) {
	t.Parallel()

	net := &flakyNet{}
	net.down.Store(true)
	// 10s / 2,5s = 4 envios por Send.
	c, _ := newClient(t, "http://argos.invalid", func(o *Options) {
		o.HTTPClient = net.client()
		o.Interval = 10 * time.Second
	})
	for ts := int64(1); ts <= 9; ts++ {
		_ = c.Send(context.Background(), report(ts))
	}
	net.down.Store(false)
	if err := c.Send(context.Background(), report(10)); err != nil {
		t.Fatal(err)
	}
	if net.delivered.Load() != 4 || c.Buffered() != 6 {
		t.Errorf("delivered=%d buffered=%d, want 4 y 6 (el resto sale en los ciclos siguientes)", net.delivered.Load(), c.Buffered())
	}
}
