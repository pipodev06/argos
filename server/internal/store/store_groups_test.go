package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const missingUUID = "00000000-0000-4000-8000-000000000000"

func hostID(t *testing.T, name string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `SELECT id FROM hosts WHERE hostname = $1`, name).Scan(&id); err != nil {
		t.Fatalf("host %q: %v", name, err)
	}
	return id
}

func mkGroup(t *testing.T, s *Store, name string, alwaysOn bool, downAfter *int) Group {
	t.Helper()
	g, err := s.CreateGroup(context.Background(), name, downAfter, alwaysOn, nil)
	if err != nil {
		t.Fatalf("creando grupo %q: %v", name, err)
	}
	return g
}

func setGroupOf(t *testing.T, host, groupID string) {
	t.Helper()
	mustExec(t, `UPDATE hosts SET group_id = $2 WHERE hostname = $1`, host, groupID)
}

func iptr(v int) *int { return &v }

// Detector con grupos: always_on -> down; sin always_on o sin grupo -> offline; umbral por grupo; disabled/pending intactos.
func TestMarkStaleHostsWithGroups(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	always := mkGroup(t, s, "servidores", true, nil)
	laptops := mkGroup(t, s, "laptops", false, iptr(600)) // umbral propio: 10 min
	strict := mkGroup(t, s, "criticos", true, iptr(30))   // umbral propio: 30 s

	seed := []struct {
		name, status string
		group        string
		ago          time.Duration
		want         string
	}{
		{"srv-always", "up", always.ID, 5 * time.Minute, "down"},
		{"srv-always-fresh", "up", always.ID, 30 * time.Second, "up"},
		{"laptop-5m", "up", laptops.ID, 5 * time.Minute, "up"},        // bajo su umbral de 10 min (el global de 2 min lo marcaria)
		{"laptop-11m", "up", laptops.ID, 11 * time.Minute, "offline"}, // sobre su umbral
		{"critico-40s", "up", strict.ID, 40 * time.Second, "down"},    // umbral 30 s < global
		{"sin-grupo", "up", "", 5 * time.Minute, "offline"},
		{"disabled-viejo", "disabled", always.ID, time.Hour, "disabled"},
		{"pending-viejo", "pending", always.ID, time.Hour, "pending"},
	}
	for _, r := range seed {
		seedHost(t, r.name, r.status, ago(r.ago))
		if r.group != "" {
			setGroupOf(t, r.name, r.group)
		}
	}
	stale, err := s.MarkStaleHosts(ctx, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	returned := map[string]string{}
	for _, h := range stale {
		returned[h.Hostname] = h.Status
	}
	for _, r := range seed {
		if got, _ := hostState(t, r.name); got != r.want {
			t.Errorf("%s: status got %q, want %q", r.name, got, r.want)
		}
		if r.want != r.status && returned[r.name] != r.want {
			t.Errorf("%s: StaleHost.Status got %q, want %q", r.name, returned[r.name], r.want)
		}
		if r.want == r.status {
			if _, ok := returned[r.name]; ok {
				t.Errorf("%s: no debia devolverse como marcado", r.name)
			}
		}
	}
}

// Recuperacion desde offline y down: el ingest los devuelve a up.
func TestIngestRecoversFromOfflineAndDown(t *testing.T) {
	s := newTestStore(t)
	for _, st := range []string{"offline", "down"} {
		seedHost(t, "h-"+st, st, ago(time.Hour))
		res, err := s.IngestMetrics(context.Background(), hostID(t, "h-"+st), Sample{Timestamp: time.Now()})
		if err != nil || !res.Recovered || res.PreviousStatus != st {
			t.Errorf("%s: got (%+v, %v), want Recovered con PreviousStatus=%s", st, res, err, st)
		}
		if got, _ := hostState(t, "h-"+st); got != "up" {
			t.Errorf("%s: status got %q, want up", st, got)
		}
	}
}

// Cambiar el grupo o always_on de un host ya down/offline lo reclasifica sin mover status_changed_at;
// up, pending y disabled no se tocan.
func TestReclassifyOnGroupChange(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	always := mkGroup(t, s, "siempre", true, nil)
	normal := mkGroup(t, s, "normal", false, nil)

	seedHost(t, "caido", "down", ago(time.Hour))
	seedHost(t, "dormido", "offline", ago(time.Hour))
	seedHost(t, "vivo", "up", ago(time.Second))
	seedHost(t, "off", "disabled", ago(time.Hour))
	seedHost(t, "pend", "pending", nil)
	_, changedBefore := hostState(t, "caido")

	// SetHostGroup: down en un grupo no always_on -> offline.
	res, err := s.SetHostGroup(ctx, hostID(t, "caido"), &normal.ID)
	if err != nil || res.Status != "offline" || res.Group == nil || res.Group.Name != "normal" {
		t.Fatalf("SetHostGroup caido -> normal got (%+v, %v), want offline", res, err)
	}
	if _, changed := hostState(t, "caido"); !changed.Equal(changedBefore) {
		t.Errorf("la reclasificacion no debe mover status_changed_at")
	}
	// offline en grupo always_on -> down.
	if res, err = s.SetHostGroup(ctx, hostID(t, "dormido"), &always.ID); err != nil || res.Status != "down" {
		t.Fatalf("SetHostGroup dormido -> always got (%+v, %v), want down", res, err)
	}
	// quitar el grupo a un down -> offline.
	if res, err = s.SetHostGroup(ctx, hostID(t, "dormido"), nil); err != nil || res.Status != "offline" || res.Group != nil {
		t.Fatalf("SetHostGroup dormido -> sin grupo got (%+v, %v), want offline sin grupo", res, err)
	}

	// UpdateGroup always_on: reclasifica a los hosts del grupo.
	for _, n := range []string{"caido", "dormido", "vivo", "off", "pend"} {
		setGroupOf(t, n, normal.ID)
	}
	mustExec(t, `UPDATE hosts SET status = 'offline' WHERE hostname IN ('caido', 'dormido')`)
	yes := true
	if _, err := s.UpdateGroup(ctx, normal.ID, GroupChange{AlwaysOn: &yes}); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"caido": "down", "dormido": "down", "vivo": "up", "off": "disabled", "pend": "pending"}
	for n, w := range want {
		if got, _ := hostState(t, n); got != w {
			t.Errorf("tras always_on=true, %s got %q, want %q", n, got, w)
		}
	}
	no := false
	if _, err := s.UpdateGroup(ctx, normal.ID, GroupChange{AlwaysOn: &no}); err != nil {
		t.Fatal(err)
	}
	want["caido"], want["dormido"] = "offline", "offline"
	for n, w := range want {
		if got, _ := hostState(t, n); got != w {
			t.Errorf("tras always_on=false, %s got %q, want %q", n, got, w)
		}
	}

	// Errores.
	missing := missingUUID
	if _, err := s.SetHostGroup(ctx, hostID(t, "vivo"), &missing); !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("grupo inexistente got %v, want ErrGroupNotFound", err)
	}
	if _, err := s.SetHostGroup(ctx, missingUUID, &normal.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("host inexistente got %v, want ErrNotFound", err)
	}
}

func TestGroupsCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	g := mkGroup(t, s, "Laptops", false, iptr(300))
	if _, err := s.CreateGroup(ctx, "laptops", nil, true, nil); !errors.Is(err, ErrGroupExists) {
		t.Errorf("nombre duplicado (sin distinguir mayusculas) got %v, want ErrGroupExists", err)
	}
	other := mkGroup(t, s, "Servidores", true, nil)
	newName := "LAPTOPS"
	if _, err := s.UpdateGroup(ctx, other.ID, GroupChange{Name: &newName}); !errors.Is(err, ErrGroupExists) {
		t.Errorf("renombrar a uno existente got %v, want ErrGroupExists", err)
	}

	// PATCH: down_after_seconds a null vuelve al global; ausente lo conserva.
	upd, err := s.UpdateGroup(ctx, g.ID, GroupChange{DownAfterSet: true})
	if err != nil || upd.DownAfterSeconds != nil || upd.Name != "Laptops" {
		t.Errorf("null explicito got (%+v, %v), want DownAfterSeconds nil y nombre intacto", upd, err)
	}
	upd, err = s.UpdateGroup(ctx, g.ID, GroupChange{DownAfterSet: true, DownAfterSeconds: iptr(90)})
	if err != nil || upd.DownAfterSeconds == nil || *upd.DownAfterSeconds != 90 {
		t.Errorf("fijar umbral got (%+v, %v)", upd, err)
	}
	no := false
	if _, err := s.UpdateGroup(ctx, missingUUID, GroupChange{AlwaysOn: &no}); !errors.Is(err, ErrNotFound) {
		t.Errorf("actualizar inexistente got %v, want ErrNotFound", err)
	}

	// Listado con conteos y sin grupo.
	seedHost(t, "a", "up", ago(time.Second))
	seedHost(t, "b", "up", ago(time.Second))
	seedHost(t, "c", "up", ago(time.Second))
	setGroupOf(t, "a", g.ID)
	setGroupOf(t, "b", g.ID)
	groups, ungrouped, err := s.ListGroups(ctx, AllScope())
	if err != nil || len(groups) != 2 || ungrouped != 1 {
		t.Fatalf("ListGroups got (%+v, %d, %v)", groups, ungrouped, err)
	}
	if groups[0].Name != "Laptops" || groups[0].HostCount != 2 || groups[1].HostCount != 0 {
		t.Errorf("conteos got %+v", groups)
	}

	// Borrar con hosts se bloquea; vacio se borra; inexistente 404.
	if err := s.DeleteGroup(ctx, g.ID); !errors.Is(err, ErrGroupNotEmpty) {
		t.Errorf("borrar con hosts got %v, want ErrGroupNotEmpty", err)
	}
	if err := s.DeleteGroup(ctx, other.ID); err != nil {
		t.Errorf("borrar vacio: %v", err)
	}
	if err := s.DeleteGroup(ctx, other.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("borrar dos veces got %v, want ErrNotFound", err)
	}
}

func TestGroupLimit200(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i := range MaxGroups {
		mkGroup(t, s, fmt.Sprintf("g%03d", i), false, nil)
	}
	if _, err := s.CreateGroup(ctx, "uno-mas", nil, false, nil); !errors.Is(err, ErrTooManyGroups) {
		t.Fatalf("grupo 201 got %v, want ErrTooManyGroups", err)
	}
	// Tras borrar uno cabe exactamente uno mas, aun con creaciones concurrentes.
	groups, _, err := s.ListGroups(ctx, AllScope())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(ctx, groups[0].ID); err != nil {
		t.Fatal(err)
	}
	var ok, full atomic.Int32
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateGroup(ctx, fmt.Sprintf("conc-%d", i), nil, false, nil)
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, ErrTooManyGroups):
				full.Add(1)
			default:
				t.Errorf("creacion concurrente: %v", err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || full.Load() != 9 {
		t.Errorf("concurrentes ok/llenos got %d/%d, want 1/9", ok.Load(), full.Load())
	}
	if n := count(t, `SELECT count(*) FROM host_groups`); n != MaxGroups {
		t.Errorf("grupos totales got %d, want %d", n, MaxGroups)
	}
}

func TestCreateGroupWithRetention(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	g, err := s.CreateGroup(ctx, "Cliente 90", nil, false, iptr(90))
	if err != nil {
		t.Fatalf("creando grupo con retencion: %v", err)
	}
	if g.DetailRetentionDays == nil || *g.DetailRetentionDays != 90 || g.EffectiveDetailDays != 90 || g.RetentionSource != SourceGroup {
		t.Errorf("retencion al crear = %v / %d / %q, quiero 90 / 90 / group", g.DetailRetentionDays, g.EffectiveDetailDays, g.RetentionSource)
	}
	if _, err := s.CreateGroup(ctx, "Cliente 7", nil, false, iptr(7)); err == nil {
		t.Error("una retencion fuera de 8..730 debe fallar por el CHECK de la tabla")
	}
	plain := mkGroup(t, s, "Sin retencion", false, nil)
	if plain.DetailRetentionDays != nil || plain.RetentionSource == SourceGroup {
		t.Errorf("sin retencion propia debe heredar la global: %v / %q", plain.DetailRetentionDays, plain.RetentionSource)
	}
}
