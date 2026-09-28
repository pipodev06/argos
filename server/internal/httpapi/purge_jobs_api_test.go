package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// finishPurge comprueba que rec es un 202 con un trabajo, ejecuta las bajas en cola (lo que hace el worker) y devuelve
// ese trabajo ya terminado, leido por GET /purge-jobs/{id}.
func (e *env) finishPurge(rec *httptest.ResponseRecorder, cookie *http.Cookie) purgeJobDTO {
	e.t.Helper()
	e.wantStatus(rec, http.StatusAccepted, "")
	var out struct {
		Job purgeJobDTO `json:"job"`
	}
	e.dataAs(rec, &out)
	if err := e.api.runPurgeJobsOnce(context.Background()); err != nil {
		e.t.Fatalf("ejecutando las bajas en cola: %v", err)
	}
	got := e.do("GET", "/api/v1/purge-jobs/"+out.Job.ID, "", withCookie(cookie))
	e.wantStatus(got, http.StatusOK, "")
	e.dataAs(got, &out)
	if out.Job.State != "done" {
		e.t.Fatalf("baja %s: estado %q (error %v), want done", out.Job.ID, out.Job.State, out.Job.Error)
	}
	return out.Job
}

func unmarshalResult(t *testing.T, job purgeJobDTO, dst any) {
	t.Helper()
	if err := json.Unmarshal(job.Result, dst); err != nil {
		t.Fatalf("resultado de la baja no coincide con %T: %v (%s)", dst, err, job.Result)
	}
}

func TestPurgeJobLifecycleAPI(t *testing.T) {
	x := newScopeEnv(t)
	seedHostData(t, x.hostA)
	body := `{"confirm_hostname":"host-a"}`
	post := func() *httptest.ResponseRecorder {
		return x.do("POST", "/api/v1/hosts/"+x.hostA+"/purge", body, withCookie(x.admin))
	}
	type jobResp struct {
		Job purgeJobDTO `json:"job"`
	}

	rec := post()
	x.wantStatus(rec, http.StatusAccepted, "")
	var first jobResp
	x.dataAs(rec, &first)
	if first.Job.State != "queued" || first.Job.Kind != "host" || first.Job.TargetName != "host-a" || first.Job.TotalHosts != 1 ||
		first.Job.Result != nil && string(first.Job.Result) != "null" {
		t.Errorf("trabajo recien creado: %+v", first.Job)
	}
	if rowsOf(t, x.hostA) == 0 {
		t.Fatal("encolar la baja borro datos antes de que corra el worker")
	}
	// Repetir la peticion devuelve el mismo trabajo activo.
	var again jobResp
	rec = post()
	x.wantStatus(rec, http.StatusAccepted, "")
	x.dataAs(rec, &again)
	if again.Job.ID != first.Job.ID {
		t.Errorf("segunda peticion: trabajo %s, want %s (el activo)", again.Job.ID, first.Job.ID)
	}
	if n := x.count(`SELECT count(*) FROM purge_jobs`); n != 1 {
		t.Errorf("trabajos creados: %d, want 1", n)
	}

	// Solo admin: un viewer no ve los trabajos.
	x.wantStatus(x.do("GET", "/api/v1/purge-jobs", "", withCookie(x.va)), http.StatusForbidden, "")
	x.wantStatus(x.do("GET", "/api/v1/purge-jobs/"+first.Job.ID, "", withCookie(x.va)), http.StatusForbidden, "")
	x.wantStatus(x.do("GET", "/api/v1/purge-jobs?state=raro", "", withCookie(x.admin)), http.StatusBadRequest, "invalid_body")
	x.wantStatus(x.do("GET", "/api/v1/purge-jobs/no-uuid", "", withCookie(x.admin)), http.StatusNotFound, "not_found")
	x.wantStatus(x.do("GET", "/api/v1/purge-jobs/"+missingID, "", withCookie(x.admin)), http.StatusNotFound, "not_found")
	var list struct {
		Items []purgeJobDTO `json:"items"`
	}
	rec = x.do("GET", "/api/v1/purge-jobs?state=active", "", withCookie(x.admin))
	x.wantStatus(rec, http.StatusOK, "")
	x.dataAs(rec, &list)
	if len(list.Items) != 1 || list.Items[0].ID != first.Job.ID {
		t.Errorf("trabajos activos: %+v, want el creado", list.Items)
	}

	job := x.finishPurge(post(), x.admin)
	if job.ID != first.Job.ID || job.DoneHosts != 1 || job.FinishedAt == nil || job.Error != nil {
		t.Errorf("trabajo terminado: %+v", job)
	}
	var res struct {
		DeletedRows struct{ Metrics int64 } `json:"deleted_rows"`
	}
	unmarshalResult(t, job, &res)
	if res.DeletedRows.Metrics != 2 {
		t.Errorf("filas de metrics borradas: %d, want 2", res.DeletedRows.Metrics)
	}
	if n := rowsOf(t, x.hostA); n != 0 {
		t.Errorf("quedan %d filas del host tras la baja", n)
	}
	x.wantStatus(x.do("GET", "/api/v1/hosts/"+x.hostA, "", withCookie(x.admin)), http.StatusNotFound, "not_found")
	rec = x.do("GET", "/api/v1/purge-jobs?state=active", "", withCookie(x.admin))
	x.dataAs(rec, &list)
	if len(list.Items) != 0 {
		t.Errorf("trabajos activos tras terminar: %d, want 0", len(list.Items))
	}
	rec = x.do("GET", "/api/v1/purge-jobs", "", withCookie(x.admin))
	x.dataAs(rec, &list)
	if len(list.Items) != 1 || list.Items[0].State != "done" {
		t.Errorf("listado: %+v, want el trabajo terminado", list.Items)
	}
}

// Un destino que desaparece antes de que corra el trabajo lo deja failed con un mensaje corto (sin detalle interno).
func TestPurgeJobFailsWithShortMessage(t *testing.T) {
	x := newScopeEnv(t)
	rec := x.do("POST", "/api/v1/hosts/"+x.hostA+"/purge", `{"confirm_hostname":"host-a"}`, withCookie(x.admin))
	x.wantStatus(rec, http.StatusAccepted, "")
	var out struct {
		Job purgeJobDTO `json:"job"`
	}
	x.dataAs(rec, &out)
	if _, err := x.st.PurgeHost(context.Background(), x.hostA, "host-a"); err != nil { // otro camino se lo lleva antes que el worker
		t.Fatal(err)
	}
	if err := x.api.runPurgeJobsOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec = x.do("GET", "/api/v1/purge-jobs/"+out.Job.ID, "", withCookie(x.admin))
	x.dataAs(rec, &out)
	if out.Job.State != "failed" || out.Job.Error == nil || *out.Job.Error != "el destino ya no existe" || out.Job.Result != nil && string(out.Job.Result) != "null" {
		t.Errorf("trabajo fallido: %+v", out.Job)
	}
}

func TestPurgeGroupJobExclusiveAPI(t *testing.T) {
	x := newScopeEnv(t)
	enqueue := func(id, name string) *httptest.ResponseRecorder {
		return x.do("POST", "/api/v1/groups/"+id+"/purge", `{"confirm_name":"`+name+`"}`, withCookie(x.admin))
	}
	rec := enqueue(x.groupA, "A")
	x.wantStatus(rec, http.StatusAccepted, "")
	var a, again struct {
		Job purgeJobDTO `json:"job"`
	}
	x.dataAs(rec, &a)
	if a.Job.Kind != "group" || a.Job.TotalHosts != 1 {
		t.Errorf("trabajo de grupo: %+v, want kind group y 1 host", a.Job)
	}
	x.wantStatus(enqueue(x.groupB, "B"), http.StatusConflict, "purge_in_progress")
	rec = enqueue(x.groupA, "A")
	x.wantStatus(rec, http.StatusAccepted, "")
	x.dataAs(rec, &again)
	if again.Job.ID != a.Job.ID {
		t.Errorf("misma baja de grupo repetida: %s, want %s", again.Job.ID, a.Job.ID)
	}
	if n := x.count(`SELECT count(*) FROM purge_jobs`); n != 1 {
		t.Errorf("trabajos: %d, want 1 (la baja de B se rechazo)", n)
	}
}

// El worker real reanuda al arrancar un trabajo que quedo en curso (reinicio) y para al cancelar su contexto.
func TestPurgeWorkerResumesInterruptedJobAndStops(t *testing.T) {
	x := newScopeEnv(t)
	rec := x.do("POST", "/api/v1/hosts/"+x.hostA+"/purge", `{"confirm_hostname":"host-a"}`, withCookie(x.admin))
	x.wantStatus(rec, http.StatusAccepted, "")
	var out struct {
		Job purgeJobDTO `json:"job"`
	}
	x.dataAs(rec, &out)
	mustExec(t, `UPDATE purge_jobs SET state = 'running', heartbeat_at = now() WHERE id = $1`, out.Job.ID)

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		x.api.RunPurgeWorker(ctx)
	}()
	deadline := time.Now().Add(15 * time.Second)
	for {
		x.dataAs(x.do("GET", "/api/v1/purge-jobs/"+out.Job.ID, "", withCookie(x.admin)), &out)
		if out.Job.State == "done" || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("el worker no paro al cancelar su contexto")
	}
	if out.Job.State != "done" {
		t.Errorf("trabajo interrumpido tras arrancar el worker: %q, want done", out.Job.State)
	}
}
