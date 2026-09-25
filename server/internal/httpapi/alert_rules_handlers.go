package httpapi

import (
	"math"
	"net/http"
	"strings"
	"time"

	"argos/server/internal/store"
)

// Rangos de los campos de una regla: alineados con los CHECK de la migracion 00015.
const (
	maxRuleNameLen     = 80
	maxRuleSeconds     = 86400
	defaultResolveSecs = 60
	minNoDataSeconds   = 60
	maxNoDataSeconds   = 30 * 24 * 3600
	// maxRuleBodyBytes: 100 puertos esperados en JSON (con espacios) no caben en maxSmallBodyBytes (4 KiB).
	maxRuleBodyBytes = 16 << 10
	maxPctThreshold  = 100
	maxLoadThreshold = 1e6
)

var (
	validRuleKinds   = map[string]bool{"metric": true, "host_status": true, "no_data": true, "service_check": true, "exposed_port": true}
	validRuleMetrics = map[string]bool{"cpu_pct": true, "mem_pct": true, "disk_max_pct": true, "battery_pct": true, "load1": true, "load5": true, "load15": true}
	validRuleOps     = map[string]bool{"gt": true, "lt": true}
	validScopeTypes  = map[string]bool{store.ScopeAll: true, store.ScopeGroup: true}
)

// --- alcance (regla y ventana de mantenimiento) ---

type scopeDTO struct {
	Type      string  `json:"type"`
	GroupID   *string `json:"group_id"`
	GroupName *string `json:"group_name"`
	// DeviceID y DeviceName solo aparecen en chequeos ping con alcance de un dispositivo.
	DeviceID   *string `json:"device_id,omitempty"`
	DeviceName *string `json:"device_name,omitempty"`
}

func toScopeDTO(s store.AlertScope, groupName *string) scopeDTO {
	return scopeDTO{Type: s.Type, GroupID: s.GroupID, GroupName: groupName, DeviceID: s.HostID}
}

// scopeReq es el alcance que manda el cliente (el nombre del grupo solo se devuelve, no se acepta).
type scopeReq struct {
	Type     string  `json:"type"`
	GroupID  *string `json:"group_id"`
	DeviceID *string `json:"device_id"`
}

// toScope valida el alcance y lo normaliza: el tipo fija que campos van (los demas deben faltar).
func (s scopeReq) toScope() (store.AlertScope, error) { return s.toScopeWith(false) }

// toScopeWith es toScope; allowDevice admite el alcance de un dispositivo concreto (solo chequeos ping).
func (s scopeReq) toScopeWith(allowDevice bool) (store.AlertScope, error) {
	if s.Type == store.ScopeDevice && allowDevice {
		if s.GroupID != nil || s.DeviceID == nil || !isUUID(*s.DeviceID) {
			return store.AlertScope{}, badRequest("scope device requiere device_id valido y no admite group_id")
		}
		id := strings.ToLower(*s.DeviceID)
		return store.AlertScope{Type: s.Type, HostID: &id}, nil
	}
	if !validScopeTypes[s.Type] {
		if allowDevice {
			return store.AlertScope{}, badRequest("scope.type debe ser all, group o device")
		}
		return store.AlertScope{}, badRequest("scope.type debe ser all o group")
	}
	if s.DeviceID != nil {
		return store.AlertScope{}, badRequest("device_id solo aplica al scope device")
	}
	out := store.AlertScope{Type: s.Type}
	switch s.Type {
	case store.ScopeAll:
		if s.GroupID != nil {
			return store.AlertScope{}, badRequest("scope all no admite group_id")
		}
	case store.ScopeGroup:
		if s.GroupID == nil || !isUUID(*s.GroupID) {
			return store.AlertScope{}, badRequest("scope.group_id requerido y debe ser un id valido")
		}
		id := strings.ToLower(*s.GroupID)
		out.GroupID = &id
	}
	return out, nil
}

// --- reglas ---

type alertRuleDTO struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Kind            string            `json:"kind"`
	Metric          *string           `json:"metric"`
	Op              *string           `json:"op"`
	Threshold       *float64          `json:"threshold"`
	ClearThreshold  *float64          `json:"clear_threshold"`
	DurationSeconds int               `json:"duration_seconds"`
	ResolveSeconds  int               `json:"resolve_seconds"`
	IncludeOffline  bool              `json:"include_offline"`
	CheckID         *string           `json:"check_id"`
	CheckName       *string           `json:"check_name"`
	WarnOrDown      bool              `json:"warn_or_down"`
	ExpectedPorts   []expectedPortDTO `json:"expected_ports"`
	Scope           scopeDTO          `json:"scope"`
	Severity        string            `json:"severity"`
	Enabled         bool              `json:"enabled"`
	ActiveAlerts    int               `json:"active_alerts"`
	ChannelIDs      []string          `json:"channel_ids"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

func toAlertRuleDTO(r store.AlertRule) alertRuleDTO {
	return alertRuleDTO{
		ID: r.ID, Name: r.Name, Kind: r.Kind, Metric: r.Metric, Op: r.Op, Threshold: r.Threshold, ClearThreshold: r.ClearThreshold,
		DurationSeconds: r.DurationSeconds, ResolveSeconds: r.ResolveSeconds, IncludeOffline: r.IncludeOffline,
		CheckID: r.CheckID, CheckName: r.CheckName, WarnOrDown: r.WarnOrDown, ExpectedPorts: toExpectedPortDTOs(r.ExpectedPorts),
		Scope: toScopeDTO(r.Scope, r.GroupName), Severity: r.Severity, Enabled: r.Enabled, ActiveAlerts: r.ActiveAlerts, ChannelIDs: r.ChannelIDs,
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

// validateRule comprueba una regla completa (nueva o ya fusionada con un PATCH) con las mismas reglas que los
// CHECK de la BD, y normaliza el nombre. Los errores son 400 con el campo culpable.
func validateRule(r *store.AlertRule) error {
	name, err := checkName("name", r.Name, maxRuleNameLen)
	if err != nil {
		return err
	}
	r.Name = name
	if !validRuleKinds[r.Kind] {
		return badRequest("kind debe ser metric, host_status, no_data, service_check o exposed_port")
	}
	if !validAlertSeverity[r.Severity] {
		return badRequest("severity debe ser info, warn o critical")
	}
	if r.DurationSeconds < 0 || r.DurationSeconds > maxRuleSeconds {
		return badRequest("duration_seconds debe estar entre 0 y %d", maxRuleSeconds)
	}
	if r.ResolveSeconds < 0 || r.ResolveSeconds > maxRuleSeconds {
		return badRequest("resolve_seconds debe estar entre 0 y %d", maxRuleSeconds)
	}
	return validateRuleCondition(r)
}

func validateRuleCondition(r *store.AlertRule) error {
	if r.Kind != "exposed_port" && len(r.ExpectedPorts) > 0 {
		return badRequest("expected_ports solo aplica a reglas exposed_port")
	}
	if r.Kind != "service_check" && (r.CheckID != nil || r.WarnOrDown) {
		return badRequest("check_id y warn_or_down solo aplican a reglas service_check")
	}
	switch r.Kind {
	case "service_check":
		if r.Metric != nil || r.Op != nil || r.Threshold != nil || r.ClearThreshold != nil {
			return badRequest("una regla service_check no admite metric, op, threshold ni clear_threshold")
		}
		if r.CheckID == nil || !isUUID(*r.CheckID) {
			return badRequest("check_id requerido y debe ser un id valido")
		}
	case "exposed_port":
		if r.Metric != nil || r.Op != nil || r.Threshold != nil || r.ClearThreshold != nil {
			return badRequest("una regla exposed_port no admite metric, op, threshold ni clear_threshold")
		}
		return validateExpectedPorts(r.ExpectedPorts)
	case "host_status":
		if r.Metric != nil || r.Op != nil || r.Threshold != nil || r.ClearThreshold != nil {
			return badRequest("una regla host_status no admite metric, op, threshold ni clear_threshold")
		}
	case "no_data":
		if r.Metric != nil || r.Op != nil || r.ClearThreshold != nil {
			return badRequest("una regla no_data no admite metric, op ni clear_threshold")
		}
		if r.Threshold == nil || !finite(*r.Threshold) || *r.Threshold < minNoDataSeconds || *r.Threshold > maxNoDataSeconds {
			return badRequest("threshold (segundos sin reportar) debe estar entre %d y %d", minNoDataSeconds, maxNoDataSeconds)
		}
	case "metric":
		if r.Metric == nil || !validRuleMetrics[*r.Metric] {
			return badRequest("metric debe ser cpu_pct, mem_pct, disk_max_pct, battery_pct, load1, load5 o load15")
		}
		if r.Op == nil || !validRuleOps[*r.Op] {
			return badRequest("op debe ser gt o lt")
		}
		limit := float64(maxPctThreshold)
		if strings.HasPrefix(*r.Metric, "load") {
			limit = maxLoadThreshold
		}
		if r.Threshold == nil || !finite(*r.Threshold) || *r.Threshold < 0 || *r.Threshold > limit {
			return badRequest("threshold debe estar entre 0 y %g", limit)
		}
		if c := r.ClearThreshold; c != nil {
			if !finite(*c) || *c < 0 || *c > limit {
				return badRequest("clear_threshold debe estar entre 0 y %g", limit)
			}
			if (*r.Op == "gt" && *c > *r.Threshold) || (*r.Op == "lt" && *c < *r.Threshold) {
				return badRequest("clear_threshold debe quedar del lado sano del threshold (<= con gt, >= con lt)")
			}
		}
	}
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func (a *API) listAlertRules(w http.ResponseWriter, r *http.Request) error {
	rules, err := a.store.ListAlertRules(r.Context(), currentUser(r).Scope)
	if err != nil {
		return err
	}
	items := make([]alertRuleDTO, 0, len(rules))
	for _, x := range rules {
		items = append(items, toAlertRuleDTO(x))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

func (a *API) getAlertRule(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	rule, err := a.store.AlertRuleByID(r.Context(), currentUser(r).Scope, id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"rule": toAlertRuleDTO(rule)})
	return nil
}

type createAlertRuleReq struct {
	Name            string            `json:"name"`
	Kind            string            `json:"kind"`
	Metric          *string           `json:"metric"`
	Op              *string           `json:"op"`
	Threshold       *float64          `json:"threshold"`
	ClearThreshold  *float64          `json:"clear_threshold"`
	DurationSeconds *int              `json:"duration_seconds"`
	ResolveSeconds  *int              `json:"resolve_seconds"`
	IncludeOffline  bool              `json:"include_offline"`
	CheckID         *string           `json:"check_id"`
	WarnOrDown      bool              `json:"warn_or_down"`
	ExpectedPorts   []expectedPortDTO `json:"expected_ports"`
	Scope           *scopeReq         `json:"scope"`
	Severity        string            `json:"severity"`
	Enabled         *bool             `json:"enabled"`
}

func (a *API) createAlertRule(w http.ResponseWriter, r *http.Request) error {
	var req createAlertRuleReq
	if err := decodeJSON(w, r, &req, maxRuleBodyBytes, true); err != nil {
		return err
	}
	if req.DurationSeconds == nil {
		return badRequest("duration_seconds requerido")
	}
	rule := store.AlertRule{
		Name: req.Name, Kind: req.Kind, Metric: req.Metric, Op: req.Op, Threshold: req.Threshold, ClearThreshold: req.ClearThreshold,
		DurationSeconds: *req.DurationSeconds, ResolveSeconds: defaultResolveSecs, IncludeOffline: req.IncludeOffline,
		CheckID: req.CheckID, WarnOrDown: req.WarnOrDown, ExpectedPorts: fromExpectedPortDTOs(req.ExpectedPorts),
		Scope: store.AlertScope{Type: store.ScopeAll}, Severity: req.Severity, Enabled: true,
	}
	if rule.CheckID != nil {
		id := strings.ToLower(*rule.CheckID)
		rule.CheckID = &id
	}
	if req.ResolveSeconds != nil {
		rule.ResolveSeconds = *req.ResolveSeconds
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	if req.Scope != nil {
		sc, err := req.Scope.toScope()
		if err != nil {
			return err
		}
		rule.Scope = sc
	}
	if err := validateRule(&rule); err != nil {
		return err
	}

	u := currentUser(r)
	created, err := a.store.CreateAlertRule(r.Context(), rule, u.ID)
	if err != nil {
		return err
	}
	e := actor(u)
	e.Event, e.Outcome, e.TargetType, e.TargetID = evAlertRuleCreated, store.OutcomeOK, "alert_rule", created.ID
	a.audit(r, e, ruleAuditDetail(created, nil))

	writeJSON(w, http.StatusCreated, map[string]any{"rule": toAlertRuleDTO(created)})
	return nil
}

// updateAlertRuleReq: PATCH parcial. kind, metric y check_id no se pueden cambiar (campo desconocido = 400): una regla con
// alertas abiertas no cambia de naturaleza; se crea otra.
type updateAlertRuleReq struct {
	Name            *string            `json:"name"`
	Op              *string            `json:"op"`
	Threshold       *float64           `json:"threshold"`
	ClearThreshold  optional[float64]  `json:"clear_threshold"`
	DurationSeconds *int               `json:"duration_seconds"`
	ResolveSeconds  *int               `json:"resolve_seconds"`
	IncludeOffline  *bool              `json:"include_offline"`
	WarnOrDown      *bool              `json:"warn_or_down"`
	ExpectedPorts   *[]expectedPortDTO `json:"expected_ports"`
	Scope           *scopeReq          `json:"scope"`
	Severity        *string            `json:"severity"`
	Enabled         *bool              `json:"enabled"`
}

func (a *API) updateAlertRule(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	var req updateAlertRuleReq
	if err := decodeJSON(w, r, &req, maxRuleBodyBytes, true); err != nil {
		return err
	}
	fields := changedRuleFields(req)
	if len(fields) == 0 {
		return badRequest("nada que cambiar")
	}
	var newScope *store.AlertScope
	if req.Scope != nil {
		sc, err := req.Scope.toScope()
		if err != nil {
			return err
		}
		newScope = &sc
	}

	updated, err := a.store.UpdateAlertRule(r.Context(), id, func(rule *store.AlertRule) error {
		applyRulePatch(rule, req, newScope)
		return validateRule(rule)
	})
	if err != nil {
		return err
	}
	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evAlertRuleUpdated, store.OutcomeOK, "alert_rule", updated.ID
	a.audit(r, e, ruleAuditDetail(updated, fields))

	writeJSON(w, http.StatusOK, map[string]any{"rule": toAlertRuleDTO(updated)})
	return nil
}

func applyRulePatch(rule *store.AlertRule, req updateAlertRuleReq, scope *store.AlertScope) {
	if req.Name != nil {
		rule.Name = *req.Name
	}
	if req.Op != nil {
		rule.Op = req.Op
	}
	if req.Threshold != nil {
		rule.Threshold = req.Threshold
	}
	if req.ClearThreshold.Set {
		rule.ClearThreshold = req.ClearThreshold.Value
	}
	if req.DurationSeconds != nil {
		rule.DurationSeconds = *req.DurationSeconds
	}
	if req.ResolveSeconds != nil {
		rule.ResolveSeconds = *req.ResolveSeconds
	}
	if req.IncludeOffline != nil {
		rule.IncludeOffline = *req.IncludeOffline
	}
	if req.WarnOrDown != nil {
		rule.WarnOrDown = *req.WarnOrDown
	}
	if req.ExpectedPorts != nil {
		rule.ExpectedPorts = fromExpectedPortDTOs(*req.ExpectedPorts)
	}
	if scope != nil {
		rule.Scope = *scope
	}
	if req.Severity != nil {
		rule.Severity = *req.Severity
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
}

// changedRuleFields lista los nombres de los campos que el PATCH toca (pa la auditoria: nunca los valores).
func changedRuleFields(req updateAlertRuleReq) []string {
	var out []string
	add := func(present bool, name string) {
		if present {
			out = append(out, name)
		}
	}
	add(req.Name != nil, "name")
	add(req.Op != nil, "op")
	add(req.Threshold != nil, "threshold")
	add(req.ClearThreshold.Set, "clear_threshold")
	add(req.DurationSeconds != nil, "duration_seconds")
	add(req.ResolveSeconds != nil, "resolve_seconds")
	add(req.IncludeOffline != nil, "include_offline")
	add(req.WarnOrDown != nil, "warn_or_down")
	add(req.ExpectedPorts != nil, "expected_ports")
	add(req.Scope != nil, "scope")
	add(req.Severity != nil, "severity")
	add(req.Enabled != nil, "enabled")
	return out
}

// ruleAuditDetail es el detalle de auditoria de una regla: el id lo lleva el target; solo nombre, tipo, metrica,
// severidad, estado y alcance (de las etiquetas, solo la clave).
func ruleAuditDetail(r store.AlertRule, fields []string) map[string]any {
	d := map[string]any{"name": r.Name, "kind": r.Kind, "severity": r.Severity, "enabled": r.Enabled, "scope_type": r.Scope.Type}
	if r.Metric != nil {
		d["metric"] = *r.Metric
	}
	if r.CheckID != nil {
		d["check_id"] = *r.CheckID
	}
	if r.Scope.GroupID != nil {
		d["group_id"] = *r.Scope.GroupID
	}
	if fields != nil {
		d["fields"] = fields
	}
	return d
}

func (a *API) deleteAlertRule(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	name, err := a.store.DeleteAlertRule(r.Context(), id)
	if err != nil {
		return err
	}
	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evAlertRuleDeleted, store.OutcomeOK, "alert_rule", id
	a.audit(r, e, map[string]any{"name": name})
	writeJSON(w, http.StatusOK, map[string]any{})
	return nil
}

// --- puertos esperados (kind exposed_port) ---

type expectedPortDTO struct {
	Proto string `json:"proto"`
	Port  int    `json:"port"`
}

func toExpectedPortDTOs(in []store.ExpectedPort) []expectedPortDTO {
	out := make([]expectedPortDTO, len(in))
	for i, p := range in {
		out[i] = expectedPortDTO{Proto: p.Proto, Port: p.Port}
	}
	return out
}

func fromExpectedPortDTOs(in []expectedPortDTO) []store.ExpectedPort {
	out := make([]store.ExpectedPort, len(in))
	for i, p := range in {
		out[i] = store.ExpectedPort{Proto: p.Proto, Port: p.Port}
	}
	return out
}

// validateExpectedPorts: max 100, proto tcp|udp, puerto 1-65535, sin repetidos.
func validateExpectedPorts(ports []store.ExpectedPort) error {
	if len(ports) > store.MaxExpectedPorts {
		return badRequest("expected_ports admite como maximo %d puertos", store.MaxExpectedPorts)
	}
	seen := make(map[store.ExpectedPort]bool, len(ports))
	for _, p := range ports {
		if p.Proto != "tcp" && p.Proto != "udp" {
			return badRequest("expected_ports[].proto debe ser tcp o udp")
		}
		if p.Port < 1 || p.Port > 65535 {
			return badRequest("expected_ports[].port debe estar entre 1 y 65535")
		}
		if seen[p] {
			return badRequest("expected_ports no admite puertos repetidos")
		}
		seen[p] = true
	}
	return nil
}
