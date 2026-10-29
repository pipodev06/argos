package sender

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"
)

// liveNet registra los ts recibidos y responde con status(ts); status 0 = error de red.
type liveNet struct {
	status func(ts int64) int
	got    []int64
}

func (l *liveNet) client() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var rep Report
		if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
			return nil, err
		}
		l.got = append(l.got, rep.Timestamp)
		code := l.status(rep.Timestamp)
		if code == 0 {
			return nil, errors.New("sin red")
		}
		return &http.Response{StatusCode: code, Body: http.NoBody, Header: http.Header{}}, nil
	})}
}

// bufferOld deja los ts 1..n en el buffer con la red caida y devuelve el cliente.
func bufferOld(t *testing.T, n int64, l *liveNet, mutate func(*Options)) *Client {
	t.Helper()
	down := true
	prev := l.status
	l.status = func(ts int64) int {
		if down {
			return 0
		}
		return prev(ts)
	}
	c, _ := newClient(t, "http://argos.invalid", func(o *Options) {
		o.HTTPClient = l.client()
		if mutate != nil {
			mutate(o)
		}
	})
	for ts := int64(1); ts <= n; ts++ {
		_ = c.Send(context.Background(), report(ts))
	}
	down = false
	l.got = nil
	return c
}

func ok(int64) int { return http.StatusOK }

func TestSendLiveReportGoesBeforeBufferedOnes(t *testing.T) {
	t.Parallel()
	l := &liveNet{status: ok}
	c := bufferOld(t, 4, l, nil)
	if err := c.Send(context.Background(), report(100)); err != nil {
		t.Fatal(err)
	}
	if want := []int64{100, 1, 2, 3, 4}; !slices.Equal(l.got, want) {
		t.Errorf("orden de llegada got %v, want %v", l.got, want)
	}
	if c.Buffered() != 0 {
		t.Errorf("Buffered got %d, want 0", c.Buffered())
	}
}

func TestSendLiveReportQueuedLastWhenServerDown(t *testing.T) {
	t.Parallel()
	l := &liveNet{status: func(int64) int { return http.StatusServiceUnavailable }}
	c := bufferOld(t, 3, l, nil)
	if err := c.Send(context.Background(), report(100)); err == nil {
		t.Fatal("se esperaba error con el server caido")
	}
	if c.Buffered() != 4 {
		t.Fatalf("Buffered got %d, want 4 (nada se pierde)", c.Buffered())
	}
	// Al recuperarse, el nuevo vivo sale primero y el resto queda viejo -> nuevo.
	l.status = ok
	l.got = nil
	if err := c.Send(context.Background(), report(101)); err != nil {
		t.Fatal(err)
	}
	if want := []int64{101, 1, 2, 3, 100}; !slices.Equal(l.got, want) {
		t.Errorf("orden de llegada got %v, want %v", l.got, want)
	}
}

func TestSendLiveReportPermanentRejectionIsDroppedAndBacklogDrains(t *testing.T) {
	t.Parallel()
	l := &liveNet{status: func(ts int64) int {
		if ts == 100 {
			return http.StatusBadRequest
		}
		return http.StatusOK
	}}
	c := bufferOld(t, 3, l, nil)
	err := c.Send(context.Background(), report(100))
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusBadRequest {
		t.Fatalf("err got %v, want StatusError 400", err)
	}
	if want := []int64{100, 1, 2, 3}; !slices.Equal(l.got, want) {
		t.Errorf("orden de llegada got %v, want %v", l.got, want)
	}
	if c.Buffered() != 0 {
		t.Errorf("Buffered got %d, want 0 (el vivo se descarta, el historico se drena)", c.Buffered())
	}
}

func TestSendLiveReport401KeepsItQueued(t *testing.T) {
	t.Parallel()
	l := &liveNet{status: func(int64) int { return http.StatusUnauthorized }}
	c := bufferOld(t, 2, l, nil)
	err := c.Send(context.Background(), report(100))
	if !errors.Is(err, ErrTokenRejected) {
		t.Fatalf("err got %v, want ErrTokenRejected", err)
	}
	if c.Buffered() != 3 {
		t.Errorf("Buffered got %d, want 3 (el vivo queda en la cola)", c.Buffered())
	}
	if len(l.got) != 1 {
		t.Errorf("requests got %d, want 1 (no se drena tras un 401)", len(l.got))
	}
}

func TestSendLiveReportCountsTowardsMaxPerSend(t *testing.T) {
	t.Parallel()
	l := &liveNet{status: ok}
	// 10s / 2,1s = 4 envios por Send.
	c := bufferOld(t, 9, l, func(o *Options) { o.Interval = 10 * time.Second })
	if err := c.Send(context.Background(), report(100)); err != nil {
		t.Fatal(err)
	}
	if want := []int64{100, 1, 2, 3}; !slices.Equal(l.got, want) {
		t.Errorf("enviados got %v, want %v (el vivo cuenta pa el tope)", l.got, want)
	}
	if c.Buffered() != 6 {
		t.Errorf("Buffered got %d, want 6", c.Buffered())
	}
}
