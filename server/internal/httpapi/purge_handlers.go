package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"argos/server/internal/store"
)

// purgeWriteWindow es el plazo de escritura de POST /groups/{id}/purge: borrar todo el historial de un cliente puede
// tardar mucho mas que el WriteTimeout del servidor.
const purgeWriteWindow = 30 * time.Minute

var errPurgeBusy = &clientError{status: http.StatusConflict, code: "purge_in_progress", msg: "hay otra purga de grupo en curso: espera a que termine"}

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

// purgeHost borra un host con agente y todos sus datos (baja de un cliente). Irreversible: exige el nombre del host.
// No limpia los respaldos (se rotan a los 14 dias).
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
	c, err := a.store.PurgeHost(r.Context(), id, req.ConfirmHostname)
	if err != nil {
		return err
	}
	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evHostPurged, store.OutcomeOK, "host", id
	a.audit(r, e, map[string]any{"metrics_rows": c.Metrics})
	writeJSON(w, http.StatusOK, map[string]any{"deleted_rows": toDeletedRowsDTO(c)})
	return nil
}

type purgeGroupReq struct {
	ConfirmName  string `json:"confirm_name"`
	DeleteGroup  bool   `json:"delete_group"`
	DisableUsers bool   `json:"disable_users"`
}

// purgeGroup purga todos los hosts y dispositivos de un grupo, su configuracion propia y los permisos de sus viewers
// (baja de un cliente). Un host que falla no detiene a los demas: se lista en failed_host_ids y se puede repetir la
// llamada (idempotente). Una sola purga de grupo a la vez.
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

	select {
	case a.purgeSem <- struct{}{}:
		defer func() { <-a.purgeSem }()
	default:
		return errPurgeBusy
	}
	rc := http.NewResponseController(w)
	// Sin soporte de deadline (p. ej. httptest) no pasa nada: rige el del servidor.
	if err := rc.SetWriteDeadline(time.Now().Add(purgeWriteWindow)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		a.logger.WarnContext(r.Context(), "no se pudo ampliar el plazo de escritura de la purga", "err", err)
	}

	res, err := a.store.PurgeGroup(r.Context(), id, store.PurgeGroupOpts{
		ConfirmName: req.ConfirmName, DeleteGroup: req.DeleteGroup, DisableUsers: req.DisableUsers,
	})
	e := actor(currentUser(r))
	e.Event, e.TargetType, e.TargetID = evGroupPurged, "group", id
	detail := purgeGroupAudit(res)
	if err != nil {
		// Una purga interrumpida ya borro parte: queda constancia aunque la respuesta sea un error.
		if res.Agents+res.Devices > 0 || res.Rows.Metrics > 0 {
			e.Outcome = store.OutcomeFail
			detail["reason"] = "interrupted"
			a.writeAudit(context.WithoutCancel(r.Context()), e, detail)
		}
		return err
	}
	e.Outcome = store.OutcomeOK
	a.audit(r, e, detail)

	failed := res.Failed
	if failed == nil {
		failed = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agents": res.Agents, "devices": res.Devices, "deleted_rows": toDeletedRowsDTO(res.Rows),
		"failed_host_ids": failed, "rules": res.Rules, "windows": res.Windows, "checks": res.Checks,
		"enroll_tokens": res.EnrollTokens, "channels": res.Channels, "snmp_profiles": res.SnmpProfiles,
		"users_disabled": res.UsersDisabled, "users_scope_reduced": res.UsersScopeReduced,
		"group_deleted": res.GroupDeleted,
	})
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
