package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"argos/agent/internal/state"
)

// srvStub responde con el status configurado y registra los timestamps recibidos, en orden.
type srvStub struct {
	*httptest.Server
	mu     sync.Mutex
	status int
	got    []int64
}

func newSrvStub(t *testing.T, status int) *srvStub {
	t.Helper()
	s := &srvStub{status: status}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var rep Report
		_ = json.NewDecoder(r.Body).Decode(&rep)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.status == http.StatusOK {
			s.got = append(s.got, rep.Timestamp)
		}
		w.WriteHeader(s.status)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *srvStub) set(status int) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

func (s *srvStub) delivered() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.got)
}

func withStore(dir string, logs *bytes.Buffer, mutate func(*Options)) func(*Options) {
	return func(o *Options) {
		if logs != nil {
			o.Logger = slog.New(slog.NewJSONHandler(logs, nil))
		}
		o.Store = state.New(dir, slog.New(slog.DiscardHandler))
		o.MaxAttempts = 1
		o.DrainPace = time.Nanosecond
		if mutate != nil {
			mutate(o)
		}
	}
}

func TestJournalSurvivesRestartAndDrainsOldestFirst(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	srv := newSrvStub(t, http.StatusServiceUnavailable)

	c1, _ := newClient(t, srv.URL, withStore(dir, nil, nil))
	for ts := int64(1); ts <= 3; ts++ {
		if err := c1.Send(context.Background(), report(ts)); err == nil {
			t.Fatal("con 503 Send debe devolver error")
		}
	}
	c1.Close() // "reinicio": el proceso nuevo solo tiene el disco

	c2, _ := newClient(t, srv.URL, withStore(dir, nil, nil))
	if c2.Buffered() != 3 {
		t.Fatalf("Buffered tras reinicio got %d, want 3", c2.Buffered())
	}
	srv.set(http.StatusOK)
	if err := c2.Send(context.Background(), report(4)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got, want := srv.delivered(), []int64{1, 2, 3, 4}; !slices.Equal(got, want) {
		t.Errorf("orden de reenvio got %v, want %v", got, want)
	}
	c2.Close()

	c3, _ := newClient(t, srv.URL, withStore(dir, nil, nil))
	if c3.Buffered() != 0 {
		t.Errorf("Buffered tras enviar todo got %d, want 0 (no reenviar duplicados)", c3.Buffered())
	}
	c3.Close()
}

func TestJournalCapByCountKeepsNewest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	srv := newSrvStub(t, http.StatusServiceUnavailable)
	mut := withStore(dir, nil, func(o *Options) { o.MaxBuffer = 3 })

	c1, _ := newClient(t, srv.URL, mut)
	for ts := int64(1); ts <= 5; ts++ {
		_ = c1.Send(context.Background(), report(ts))
	}
	c1.Close()

	srv.set(http.StatusOK)
	c2, _ := newClient(t, srv.URL, mut)
	if c2.Buffered() != 3 {
		t.Fatalf("Buffered got %d, want 3", c2.Buffered())
	}
	if err := c2.Send(context.Background(), report(6)); err != nil {
		t.Fatal(err)
	}
	if got, want := srv.delivered(), []int64{4, 5, 6}; !slices.Equal(got, want) {
		t.Errorf("entregados got %v, want %v (tope 3: los mas nuevos)", got, want)
	}
	c2.Close()
}

func TestJournalCapByBytes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	srv := newSrvStub(t, http.StatusServiceUnavailable)
	line, err := journalLine(report(1))
	if err != nil {
		t.Fatal(err)
	}
	// Cabe 2,5 reportes: el tercero desplaza al primero.
	mut := withStore(dir, nil, func(o *Options) { o.MaxBufferBytes = int64(len(line)) * 5 / 2 })

	c, _ := newClient(t, srv.URL, mut)
	for ts := int64(1); ts <= 6; ts++ {
		_ = c.Send(context.Background(), report(ts))
	}
	if c.Buffered() != 2 {
		t.Errorf("Buffered got %d, want 2 (tope de bytes)", c.Buffered())
	}
	c.Close()
	c2, _ := newClient(t, srv.URL, mut)
	if c2.Buffered() != 2 {
		t.Errorf("Buffered tras reinicio got %d, want 2", c2.Buffered())
	}
	c2.Close()
}

func TestJournalCorruptIsDiscardedWithoutLeakingData(t *testing.T) {
	t.Parallel()
	good, err := journalLine(report(1))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		content  []byte
		wantKept int
	}{
		{"basura", []byte("esto no es un journal\n\x00\xff"), 0},
		{"json roto en una linea completa", append(slices.Clone(good), []byte("R{\"hostname\":\n")...), 0},
		{"cola cortada por kill -9", append(slices.Clone(good), good[:len(good)/2]...), 1},
		{"vacio", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, journalName), tt.content, 0o600); err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			c, _ := newClient(t, "http://127.0.0.1:1", withStore(dir, &logs, nil))
			if c.Buffered() != tt.wantKept {
				t.Errorf("Buffered got %d, want %d", c.Buffered(), tt.wantKept)
			}
			if strings.Contains(logs.String(), "esto no es") || strings.Contains(logs.String(), "hostname") {
				t.Errorf("el log filtra contenido del archivo: %s", logs.String())
			}
			// El agente sigue funcionando: encolar tras la recuperacion no falla ni pierde lo bueno.
			_ = c.Send(context.Background(), report(9))
			if c.Buffered() != tt.wantKept+1 {
				t.Errorf("Buffered tras Send got %d, want %d", c.Buffered(), tt.wantKept+1)
			}
			c.Close()
		})
	}
}

func TestExpiredReportsAreDroppedWithoutSending(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	srv := newSrvStub(t, http.StatusServiceUnavailable)
	now := time.Unix(1_000_000_000, 0)
	mut := withStore(dir, nil, func(o *Options) {
		o.MaxAge = 6 * 24 * time.Hour
		o.Now = func() time.Time { return now }
	})
	c1, _ := newClient(t, srv.URL, mut)
	old := now.Add(-7 * 24 * time.Hour).Unix()
	fresh := now.Add(-time.Hour).Unix()
	_ = c1.Send(context.Background(), report(old))
	_ = c1.Send(context.Background(), report(fresh))
	c1.Close()

	srv.set(http.StatusOK)
	c2, _ := newClient(t, srv.URL, mut)
	if err := c2.Send(context.Background(), report(now.Unix())); err != nil {
		t.Fatal(err)
	}
	if got, want := srv.delivered(), []int64{fresh, now.Unix()}; !slices.Equal(got, want) {
		t.Errorf("entregados got %v, want %v (el vencido no se envia)", got, want)
	}
	c2.Close()
}

func TestJournalStatusHandling(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		status      int
		wantBuffers int
	}{
		{"429 se conserva", http.StatusTooManyRequests, 1},
		{"500 se conserva", http.StatusInternalServerError, 1},
		{"400 se descarta", http.StatusBadRequest, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			srv := newSrvStub(t, tt.status)
			c, _ := newClient(t, srv.URL, withStore(dir, nil, nil))
			if err := c.Send(context.Background(), report(1)); err == nil {
				t.Error("Send debe devolver error")
			}
			c.Close()
			c2, _ := newClient(t, srv.URL, withStore(dir, nil, nil))
			if c2.Buffered() != tt.wantBuffers {
				t.Errorf("Buffered tras reinicio got %d, want %d", c2.Buffered(), tt.wantBuffers)
			}
			c2.Close()
		})
	}
}

func TestJournalNeverContainsTokenAndIsPrivate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	srv := newSrvStub(t, http.StatusServiceUnavailable)
	var logs bytes.Buffer
	c, _ := newClient(t, srv.URL, withStore(dir, &logs, nil))
	_ = c.Send(context.Background(), report(1))
	c.Close()

	path := filepath.Join(dir, journalName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("el journal esta vacio")
	}
	if strings.Contains(string(data), testToken) || strings.Contains(logs.String(), testToken) {
		t.Error("el token aparece en el journal o en los logs")
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("permisos got %o, want 600", fi.Mode().Perm())
		}
	}
}

func TestJournalCompactsAndStaysBounded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	srv := newSrvStub(t, http.StatusServiceUnavailable)
	mut := withStore(dir, nil, func(o *Options) { o.MaxBuffer = 4 })
	c, _ := newClient(t, srv.URL, mut)
	big := report(0)
	big.Hostname = strings.Repeat("h", 200_000)
	// 30 envios con tope 4: cada uno descarta el mas viejo y deja lineas muertas hasta compactar.
	for ts := int64(1); ts <= 30; ts++ {
		big.Timestamp = ts
		_ = c.Send(context.Background(), big)
	}
	c.Close()
	fi, err := os.Stat(filepath.Join(dir, journalName))
	if err != nil {
		t.Fatal(err)
	}
	if limit := int64(8*200_000) + compactMinDead; fi.Size() > limit {
		t.Errorf("journal pesa %d, want <= %d (debe compactarse)", fi.Size(), limit)
	}
	c2, _ := newClient(t, srv.URL, mut)
	if c2.Buffered() != 4 || c2.q.items[0].r.Timestamp != 27 {
		t.Errorf("tras compactar: Buffered %d primero ts %d, want 4 y 27", c2.Buffered(), c2.q.items[0].r.Timestamp)
	}
	c2.Close()
}
