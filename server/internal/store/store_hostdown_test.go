package store

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

// seedHost inserta un host con last_seen_at = now() - lastSeenAgo (nil = NULL) y un
// status_changed_at fijo de hace un dia, pa poder afirmar si se movio o no.
func seedHost(t *testing.T, name, status string, lastSeenAgo *time.Duration) {
	t.Helper()
	var secs *float64
	if lastSeenAgo != nil {
		v := lastSeenAgo.Seconds()
		secs = &v
	}
	// Un host disabled siempre esta revocado (lo exige un CHECK).
	mustExec(t, `INSERT INTO hosts (hostname, agent_token_hash, status, last_seen_at, status_changed_at, agent_token_revoked_at)
		VALUES ($1, $2, $3, now() - make_interval(secs => $4::float8), now() - interval '1 day',
		        CASE WHEN $3 = 'disabled' THEN now() END)`,
		name, "hash-"+name, status, secs)
}

func ago(d time.Duration) *time.Duration { return &d }

// hostState devuelve status y status_changed_at del host por hostname.
func hostState(t *testing.T, name string) (string, time.Time) {
	t.Helper()
	var status string
	var changed time.Time
	err := testPool.QueryRow(context.Background(),
		`SELECT status, status_changed_at FROM hosts WHERE hostname = $1`, name).Scan(&status, &changed)
	if err != nil {
		t.Fatalf("leyendo host %q: %v", name, err)
	}
	return status, changed
}

func TestMarkStaleHosts(t *testing.T) {
	const downAfter = 2 * time.Minute

	tests := []struct {
		name        string
		status      string
		lastSeenAgo *time.Duration
		wantStatus  string
		wantMoved   bool // si status_changed_at debe cambiar
	}{
		{name: "up reciente no cambia", status: "up", lastSeenAgo: ago(30 * time.Second), wantStatus: "up"},
		{name: "up justo bajo el umbral no cambia", status: "up", lastSeenAgo: ago(downAfter - 30*time.Second), wantStatus: "up"},
		{name: "up viejo sin grupo pasa a offline", status: "up", lastSeenAgo: ago(3 * time.Minute), wantStatus: "offline", wantMoved: true},
		{name: "up muy viejo sin grupo pasa a offline", status: "up", lastSeenAgo: ago(72 * time.Hour), wantStatus: "offline", wantMoved: true},
		{name: "ya down no re-cambia ni mueve status_changed_at", status: "down", lastSeenAgo: ago(time.Hour), wantStatus: "down"},
		{name: "ya offline no re-cambia ni mueve status_changed_at", status: "offline", lastSeenAgo: ago(time.Hour), wantStatus: "offline"},
		{name: "disabled viejo intacto", status: "disabled", lastSeenAgo: ago(time.Hour), wantStatus: "disabled"},
		{name: "pending viejo intacto", status: "pending", lastSeenAgo: ago(time.Hour), wantStatus: "pending"},
		{name: "pending sin last_seen intacto", status: "pending", lastSeenAgo: nil, wantStatus: "pending"},
		{name: "up sin last_seen no se puede juzgar", status: "up", lastSeenAgo: nil, wantStatus: "up"},
	}

	s := newTestStore(t)
	ctx := context.Background()
	before := map[string]time.Time{}
	var wantDown []string
	for _, tt := range tests {
		seedHost(t, tt.name, tt.status, tt.lastSeenAgo)
		_, before[tt.name] = hostState(t, tt.name)
		if tt.wantMoved {
			wantDown = append(wantDown, tt.name)
		}
	}

	stale, err := s.MarkStaleHosts(ctx, downAfter)
	if err != nil {
		t.Fatalf("MarkStaleHosts: %v", err)
	}
	var gotDown []string
	for _, h := range stale {
		if h.ID == "" {
			t.Errorf("host caido sin id: %+v", h)
		}
		gotDown = append(gotDown, h.Hostname)
	}
	slices.Sort(gotDown)
	slices.Sort(wantDown)
	if !slices.Equal(gotDown, wantDown) {
		t.Errorf("hosts marcados down got %v, want %v", gotDown, wantDown)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, changed := hostState(t, tt.name)
			if status != tt.wantStatus {
				t.Errorf("status got %q, want %q", status, tt.wantStatus)
			}
			if moved := !changed.Equal(before[tt.name]); moved != tt.wantMoved {
				t.Errorf("status_changed_at movido got %v, want %v", moved, tt.wantMoved)
			}
		})
	}

	// Idempotente: una segunda pasada no encuentra nada y no mueve nada.
	again, err := s.MarkStaleHosts(ctx, downAfter)
	if err != nil || len(again) != 0 {
		t.Errorf("segunda pasada got (%v, %v), want vacio", again, err)
	}
	if _, changed := hostState(t, "up viejo sin grupo pasa a offline"); changed.Before(time.Now().Add(-time.Minute)) {
		t.Errorf("status_changed_at del host caido no se actualizo a now()")
	}
}

// Varias instancias del server corriendo el detector a la vez: cada host se marca una sola vez.
func TestMarkStaleHostsConcurrent(t *testing.T) {
	s := newTestStore(t)
	const hosts = 50
	for i := range hosts {
		seedHost(t, fmt.Sprintf("h%d", i), "up", ago(time.Hour))
	}

	const workers = 4
	counts := make([]int, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var stale []StaleHost
			stale, errs[i] = s.MarkStaleHosts(context.Background(), 2*time.Minute)
			counts[i] = len(stale)
		}()
	}
	wg.Wait()

	total := 0
	for i := range workers {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		total += counts[i]
	}
	if total != hosts {
		t.Errorf("hosts marcados entre todas las instancias got %d, want %d (%v)", total, hosts, counts)
	}
}

func TestIngestRecoveryAndStatusChangedAt(t *testing.T) {
	tests := []struct {
		name          string
		status        string
		wantStatus    string
		wantRecovered bool
		wantMoved     bool
	}{
		{name: "down vuelve a up", status: "down", wantStatus: "up", wantRecovered: true, wantMoved: true},
		{name: "offline vuelve a up", status: "offline", wantStatus: "up", wantRecovered: true, wantMoved: true},
		{name: "pending pasa a up sin ser recuperacion", status: "pending", wantStatus: "up", wantMoved: true},
		{name: "up sigue up y no mueve status_changed_at", status: "up", wantStatus: "up"},
		{name: "disabled no se reactiva", status: "disabled", wantStatus: "disabled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			seedHost(t, "srv", tt.status, ago(10*time.Minute))
			_, before := hostState(t, "srv")
			var hostID string
			if err := testPool.QueryRow(context.Background(), `SELECT id FROM hosts WHERE hostname = 'srv'`).Scan(&hostID); err != nil {
				t.Fatal(err)
			}

			res, err := s.IngestMetrics(context.Background(), hostID, Sample{Timestamp: time.Now()})
			if err != nil {
				t.Fatalf("ingest: %v", err)
			}
			if res.Recovered != tt.wantRecovered {
				t.Errorf("Recovered got %v, want %v", res.Recovered, tt.wantRecovered)
			}
			if tt.status != "disabled" && res.Hostname != "srv" {
				t.Errorf("Hostname got %q, want srv", res.Hostname)
			}
			status, changed := hostState(t, "srv")
			if status != tt.wantStatus {
				t.Errorf("status got %q, want %q", status, tt.wantStatus)
			}
			if moved := !changed.Equal(before); moved != tt.wantMoved {
				t.Errorf("status_changed_at movido got %v, want %v", moved, tt.wantMoved)
			}
			if got := count(t, `SELECT count(*) FROM metrics WHERE host_id = $1`, hostID); got != 1 {
				t.Errorf("metrics got %d, want 1 (se guardan aunque el host este disabled)", got)
			}
		})
	}
}

// Ciclo completo: enroll -> deja de reportar -> down -> reporta de nuevo -> up.
func TestHostDownAndRecoveryCycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	hostID := enrolled(t, s, "h1")

	// El enroll fija status_changed_at con el DEFAULT: coincide con last_seen_at (mismo now()).
	if got := count(t, `SELECT count(*) FROM hosts WHERE id = $1 AND status = 'up' AND status_changed_at = last_seen_at`, hostID); got != 1 {
		t.Fatalf("tras el enroll, status_changed_at debe ser el momento del enroll")
	}
	_, enrolledAt := hostState(t, "srv")

	mustExec(t, `UPDATE hosts SET last_seen_at = now() - interval '5 minutes' WHERE id = $1`, hostID)
	stale, err := s.MarkStaleHosts(ctx, 2*time.Minute)
	if err != nil || len(stale) != 1 || stale[0].ID != hostID || stale[0].Hostname != "srv" {
		t.Fatalf("detector got (%+v, %v), want el host srv", stale, err)
	}
	status, downAt := hostState(t, "srv")
	if status != "offline" || !downAt.After(enrolledAt) {
		t.Fatalf("tras el detector: status=%q down_at=%v enrolled_at=%v", status, downAt, enrolledAt)
	}

	res, err := s.IngestMetrics(ctx, hostID, Sample{Timestamp: time.Now()})
	if err != nil || !res.Recovered {
		t.Fatalf("ingest tras la caida got (%+v, %v), want Recovered", res, err)
	}
	status, upAt := hostState(t, "srv")
	if status != "up" || !upAt.After(downAt) {
		t.Fatalf("tras la recuperacion: status=%q up_at=%v down_at=%v", status, upAt, downAt)
	}

	// La API lee el campo nuevo.
	page, err := s.ListHosts(ctx, HostFilter{Sort: "severity", Limit: 10})
	if err != nil || len(page.Items) != 1 || !page.Items[0].StatusChangedAt.Equal(upAt) {
		t.Errorf("ListHosts got (%+v, %v), want StatusChangedAt=%v", page, err, upAt)
	}
	detail, err := s.HostByID(ctx, hostID)
	if err != nil || !detail.StatusChangedAt.Equal(upAt) {
		t.Errorf("HostByID got (%+v, %v), want StatusChangedAt=%v", detail, err, upAt)
	}
}
