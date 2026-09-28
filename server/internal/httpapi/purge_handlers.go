package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"argos/server/internal/store"
)

// purgeJobsListLimit es cuantos trabajos devuelve GET /purge-jobs.
const purgeJobsListLimit = 20

type purgeHostReq struct {
	ConfirmHostname string `json:"confirm_hostname"`
}

type deletedRowsDTO struct {
	Metrics       int64 `json:"metrics"`
	MetricsHourly int64 `json:"metrics_hourly"`
	MetricsChunks int64 `json:"metrics_chunks"`
	DiskSamples   int64 `json:"disk_samples"`
	DiskChunks    int64 `json:"disk_chunks"`
	ServiceChecks int64 `json:"service_checks"`
}

func toDeletedRowsDTO(c store.PurgeCounts) deletedRowsDTO {
	return deletedRowsDTO{
		Metrics: c.Metrics, MetricsHourly: c.Hourly, MetricsChunks: c.Chunks,
		DiskSamples: c.Disks, DiskChunks: c.DiskChunks, ServiceChecks: c.Checks,
	}
}

// purgeJobDTO es un trabajo de baja. Result es, al terminar bien, la respuesta que antes daba la baja sincrona.
type purgeJobDTO struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	TargetName string          `json:"target_name"`
	State      string          `json:"state"`
	TotalHosts int             `json:"total_hosts"`
	DoneHosts  int             `json:"done_hosts"`
	Error      *string         `json:"error"`
	CreatedAt  time.Time       `json:"created_at"`
	StartedAt  *time.Time      `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at"`
	Result     json.RawMessage `json:"result"`
}

func toPurgeJobDTO(j store.PurgeJob) purgeJobDTO {
	return purgeJobDTO{
		ID: j.ID, Kind: j.Kind, TargetName: j.TargetName, State: j.State, TotalHosts: j.TotalHosts, DoneHosts: j.DoneHosts,
		Error: j.Error, CreatedAt: j.CreatedAt, StartedAt: j.StartedAt, FinishedAt: j.FinishedAt, Result: j.Result,
	}
}

// enqueuePurge encola la baja ya validada a nombre del usuario de la sesion y responde 202 con el trabajo (el que
// ya estaba activo pa ese destino, si lo hay).
func (a *API) enqueuePurge(w http.ResponseWriter, r *http.Request, p store.NewPurgeJob) error {
	u := currentUser(r)
	p.RequestedBy, p.RequestedByName = u.ID, u.Username
	job, err := a.store.CreatePurgeJob(r.Context(), p)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job": toPurgeJobDTO(job)})
	return nil
}

// purgeHost encola la baja de un host con agente y todos sus datos (baja de un cliente). Irreversible: exige el nombre
// del host. No limpia los respaldos (se rotan a los 14 dias). El borrado lo hace el worker (purge_worker.go).
func (a *API) purgeHost(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return invalidID()
	}
	var req purgeHostReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	if req.ConfirmHostname == "" {
		return badRequest("confirm_hostname requerido: el nombre del host")
	}
	id = strings.ToLower(id)
	name, err := a.store.PreparePurgeHost(r.Context(), id, req.ConfirmHostname)
	if err != nil {
		return err
	}
	return a.enqueuePurge(w, r, store.NewPurgeJob{Kind: store.PurgeKindHost, TargetID: id, TargetName: name, TotalHosts: 1})
}

type purgeGroupReq struct {
	ConfirmName  string `json:"confirm_name"`
	DeleteGroup  bool   `json:"delete_group"`
	DisableUsers bool   `json:"disable_users"`
}

// purgeGroup encola la baja de todos los hosts y dispositivos de un grupo, su configuracion propia y los permisos de
// sus viewers (baja de un cliente). Un host que falla no detiene a los demas: se lista en failed_host_ids del
// resultado y se puede repetir la peticion (idempotente). Una sola baja de grupo a la vez (409 purge_in_progress).
func (a *API) purgeGroup(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	var req purgeGroupReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	if req.ConfirmName == "" {
		return badRequest("confirm_name requerido: el nombre del grupo")
	}
	id = strings.ToLower(id)
	total, err := a.store.PreparePurgeGroup(r.Context(), id, req.ConfirmName)
	if err != nil {
		return err
	}
	return a.enqueuePurge(w, r, store.NewPurgeJob{
		Kind: store.PurgeKindGroup, TargetID: id, TargetName: req.ConfirmName, TotalHosts: total,
		Options: store.PurgeJobOptions{DeleteGroup: req.DeleteGroup, DisableUsers: req.DisableUsers},
	})
}

func (a *API) getPurgeJob(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return store.ErrNotFound
	}
	job, err := a.store.PurgeJobByID(r.Context(), strings.ToLower(id))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": toPurgeJobDTO(job)})
	return nil
}

// listPurgeJobs devuelve las bajas mas recientes; ?state=active deja solo las que estan en cola o en curso.
func (a *API) listPurgeJobs(w http.ResponseWriter, r *http.Request) error {
	var activeOnly bool
	switch r.URL.Query().Get("state") {
	case "":
	case "active":
		activeOnly = true
	default:
		return badRequest("state invalido: usa active o nada")
	}
	jobs, err := a.store.ListPurgeJobs(r.Context(), activeOnly, purgeJobsListLimit)
	if err != nil {
		return err
	}
	items := make([]purgeJobDTO, len(jobs))
	for i, j := range jobs {
		items[i] = toPurgeJobDTO(j)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

// purgeGroupAudit arma el detalle de auditoria: solo conteos, nunca nombres ni ids de hosts del cliente.
func purgeGroupAudit(res store.PurgeGroupResult) map[string]any {
	return map[string]any{
		"agents": res.Agents, "devices": res.Devices, "metrics_rows": res.Rows.Metrics, "failed": len(res.Failed),
		"rules": res.Rules, "windows": res.Windows, "checks": res.Checks, "enroll_tokens": res.EnrollTokens,
		"channels": res.Channels, "snmp_profiles": res.SnmpProfiles,
		"users_disabled": res.UsersDisabled, "users_scope_reduced": res.UsersScopeReduced, "group_deleted": res.GroupDeleted,
	}
}

// groupRuleDetails devuelve el detalle (cuenta de dispositivos) de un error de la regla colector-grupo, o nil.
func groupRuleDetails(err error) any {
	var g *store.GroupRuleError
	if errors.As(err, &g) && g.Devices > 0 {
		return map[string]int{"devices": g.Devices}
	}
	return nil
}
