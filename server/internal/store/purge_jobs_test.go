package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newPurgeJob(kind, target string) NewPurgeJob {
	return NewPurgeJob{Kind: kind, TargetID: target, TargetName: "n-" + kind, RequestedByName: "adm", TotalHosts: 1}
}

const (
	targetX = "00000000-0000-4000-8000-0000000000a1"
	targetY = "00000000-0000-4000-8000-0000000000a2"
)

func TestPurgeJobCreateClaimFinish(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	uid := mkUser(t, s, "adm", "admin")

	p := newPurgeJob(PurgeKindGroup, targetX)
	p.RequestedBy, p.Options = uid, PurgeJobOptions{DeleteGroup: true}
	j, err := s.CreatePurgeJob(ctx, p)
	if err != nil || j.State != PurgeJobQueued || j.RequestedBy != uid || !j.Options.DeleteGroup || j.StartedAt != nil {
		t.Fatalf("crear: %+v, %v", j, err)
	}
	got, ok, err := s.ClaimPurgeJob(ctx)
	if err != nil || !ok || got.ID != j.ID || got.State != PurgeJobRunning || got.StartedAt == nil {
		t.Fatalf("reclamar: %+v, ok=%v, %v", got, ok, err)
	}
	if _, ok, err := s.ClaimPurgeJob(ctx); err != nil || ok {
		t.Errorf("segunda reclamacion: ok=%v, %v; want vacia", ok, err)
	}
	if err := s.UpdatePurgeProgress(ctx, j.ID, 3, 5); err != nil {
		t.Fatal(err)
	}
	mid, err := s.PurgeJobByID(ctx, j.ID)
	if err != nil || mid.DoneHosts != 3 || mid.TotalHosts != 5 {
		t.Fatalf("avance: %+v, %v", mid, err)
	}
	if err := s.FinishPurgeJob(ctx, j.ID, []byte(`{"agents":2}`), ""); err != nil {
		t.Fatal(err)
	}
	done, err := s.PurgeJobByID(ctx, j.ID)
	if err != nil || done.State != PurgeJobDone || done.DoneHosts != 5 || done.FinishedAt == nil || string(done.Result) != `{"agents": 2}` || done.Error != nil {
		t.Fatalf("terminado: %+v, %v", done, err)
	}

	// Terminado el trabajo, el mismo destino admite otro nuevo; uno fallido guarda el mensaje.
	j2, err := s.CreatePurgeJob(ctx, newPurgeJob(PurgeKindGroup, targetX))
	if err != nil || j2.ID == j.ID {
		t.Fatalf("nuevo trabajo del mismo destino: %+v, %v", j2, err)
	}
	if err := s.FinishPurgeJob(ctx, j2.ID, nil, "la baja fallo; reintenta"); err != nil {
		t.Fatal(err)
	}
	failed, err := s.PurgeJobByID(ctx, j2.ID)
	if err != nil || failed.State != PurgeJobFailed || failed.Error == nil || *failed.Error != "la baja fallo; reintenta" || failed.Result != nil {
		t.Errorf("fallido: %+v, %v", failed, err)
	}
	// Al borrarse el usuario se conserva el nombre del solicitante.
	mustExec(t, `DELETE FROM users WHERE id = $1`, uid)
	orphan, err := s.PurgeJobByID(ctx, j.ID)
	if err != nil || orphan.RequestedBy != "" || orphan.RequestedByName != "adm" {
		t.Errorf("solicitante tras borrar el usuario: %q / %q, %v", orphan.RequestedBy, orphan.RequestedByName, err)
	}
	if _, err := s.PurgeJobByID(ctx, targetY); !errors.Is(err, ErrNotFound) {
		t.Errorf("trabajo inexistente: %v, want ErrNotFound", err)
	}
}

func TestPurgeJobActiveIsUniquePerTarget(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a, err := s.CreatePurgeJob(ctx, newPurgeJob(PurgeKindHost, targetX))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreatePurgeJob(ctx, newPurgeJob(PurgeKindHost, targetX))
	if err != nil || b.ID != a.ID {
		t.Fatalf("segundo trabajo activo del mismo destino: %+v, %v; want el existente %s", b, err, a.ID)
	}
	if _, ok, err := s.ClaimPurgeJob(ctx); err != nil || !ok { // running tambien cuenta como activo
		t.Fatalf("no se pudo reclamar: ok=%v, %v", ok, err)
	}
	if c, err := s.CreatePurgeJob(ctx, newPurgeJob(PurgeKindHost, targetX)); err != nil || c.ID != a.ID {
		t.Errorf("trabajo running: %+v, %v; want el existente", c, err)
	}
	// Otro destino, y el mismo id con otro tipo, son trabajos distintos.
	for _, p := range []NewPurgeJob{newPurgeJob(PurgeKindHost, targetY), newPurgeJob(PurgeKindDevice, targetX)} {
		if d, err := s.CreatePurgeJob(ctx, p); err != nil || d.ID == a.ID {
			t.Errorf("trabajo %s/%s: %+v, %v; want uno nuevo", p.Kind, p.TargetID, d, err)
		}
	}
	// El indice unico es la red de seguridad si algo se salta CreatePurgeJob.
	if _, err := testPool.Exec(ctx, `INSERT INTO purge_jobs (kind, target_id, target_name, requested_by_name) VALUES ('host', $1, 'x', 'x')`, targetX); err == nil {
		t.Error("el indice unico debe rechazar un segundo trabajo activo del mismo destino")
	}
}

func TestPurgeJobOneGroupAtATime(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	g1, err := s.CreatePurgeJob(ctx, newPurgeJob(PurgeKindGroup, targetX))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePurgeJob(ctx, newPurgeJob(PurgeKindGroup, targetY)); !errors.Is(err, ErrPurgeBusy) {
		t.Errorf("segunda baja de grupo: %v, want ErrPurgeBusy", err)
	}
	if again, err := s.CreatePurgeJob(ctx, newPurgeJob(PurgeKindGroup, targetX)); err != nil || again.ID != g1.ID {
		t.Errorf("mismo grupo: %+v, %v; want el existente", again, err)
	}
	// Una baja de host no la bloquea, y una baja de grupo no se reclama mientras otra corre.
	if _, err := s.CreatePurgeJob(ctx, newPurgeJob(PurgeKindHost, targetY)); err != nil {
		t.Errorf("baja de host con una de grupo activa: %v", err)
	}
	mustExec(t, `UPDATE purge_jobs SET state = 'running' WHERE id = $1`, g1.ID)
	mustExec(t, `INSERT INTO purge_jobs (kind, target_id, target_name, requested_by_name) VALUES ('group', $1, 'g2', 'x')`, targetY)
	got, ok, err := s.ClaimPurgeJob(ctx)
	if err != nil || !ok || got.Kind != PurgeKindHost {
		t.Fatalf("reclamar con un grupo en curso: %+v, ok=%v, %v; want la baja de host", got, ok, err)
	}
	if _, ok, err := s.ClaimPurgeJob(ctx); err != nil || ok {
		t.Errorf("se reclamo una segunda baja de grupo mientras otra corre: ok=%v, %v", ok, err)
	}
}

func TestRequeueStalePurgeJobsAndCleanup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	old, err := s.CreatePurgeJob(ctx, newPurgeJob(PurgeKindHost, targetX))
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := s.CreatePurgeJob(ctx, newPurgeJob(PurgeKindHost, targetY))
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, `UPDATE purge_jobs SET state = 'running', heartbeat_at = now() - interval '10 minutes' WHERE id = $1`, old.ID)
	mustExec(t, `UPDATE purge_jobs SET state = 'running', heartbeat_at = now() WHERE id = $1`, fresh.ID)

	if n, err := s.RequeueStalePurgeJobs(ctx, 5*time.Minute); err != nil || n != 1 {
		t.Fatalf("reencolar viejos: %d, %v; want 1", n, err)
	}
	if j, _ := s.PurgeJobByID(ctx, old.ID); j.State != PurgeJobQueued {
		t.Errorf("trabajo con latido viejo: %s, want queued", j.State)
	}
	if j, _ := s.PurgeJobByID(ctx, fresh.ID); j.State != PurgeJobRunning {
		t.Errorf("trabajo con latido reciente: %s, want running", j.State)
	}
	if n, err := s.RequeueStalePurgeJobs(ctx, 0); err != nil || n != 1 {
		t.Fatalf("reencolar todos (arranque): %d, %v; want 1", n, err)
	}
	if j, _ := s.PurgeJobByID(ctx, fresh.ID); j.State != PurgeJobQueued {
		t.Errorf("tras el arranque: %s, want queued", j.State)
	}

	// Limpieza: solo los terminados hace mas de 30 dias.
	mustExec(t, `UPDATE purge_jobs SET state = 'done', finished_at = now() - interval '31 days' WHERE id = $1`, old.ID)
	mustExec(t, `UPDATE purge_jobs SET state = 'done', finished_at = now() - interval '2 days' WHERE id = $1`, fresh.ID)
	if n, err := s.DeleteOldPurgeJobs(ctx, 30*24*time.Hour); err != nil || n != 1 {
		t.Fatalf("limpieza: %d, %v; want 1", n, err)
	}
	jobs, err := s.ListPurgeJobs(ctx, false, 20)
	if err != nil || len(jobs) != 1 || jobs[0].ID != fresh.ID {
		t.Errorf("tras la limpieza: %+v, %v; want solo el reciente", jobs, err)
	}
	if active, err := s.ListPurgeJobs(ctx, true, 20); err != nil || len(active) != 0 {
		t.Errorf("activos: %d, %v; want 0", len(active), err)
	}
}

func TestPurgeGroupReportsProgress(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	g := mkGroup(t, s, "prog", false, nil)
	for _, name := range []string{"p1", "p2", "p3"} {
		mustExec(t, `UPDATE hosts SET group_id = $2 WHERE id = $1`, mkHost(t, name), g.ID)
	}
	if n, err := s.PreparePurgeGroup(ctx, g.ID, "prog"); err != nil || n != 3 {
		t.Fatalf("PreparePurgeGroup: %d, %v; want 3", n, err)
	}
	if _, err := s.PreparePurgeGroup(ctx, g.ID, "otro"); !errors.Is(err, ErrPurgeConfirm) {
		t.Errorf("nombre distinto: %v, want ErrPurgeConfirm", err)
	}
	var steps [][2]int
	_, err := s.PurgeGroup(ctx, g.ID, PurgeGroupOpts{ConfirmName: "prog", OnProgress: func(done, total int) { steps = append(steps, [2]int{done, total}) }})
	if err != nil || len(steps) != 3 || steps[0] != [2]int{1, 3} || steps[2] != [2]int{3, 3} {
		t.Errorf("progreso: %v, %v; want 3 pasos hasta 3 de 3", steps, err)
	}
}
