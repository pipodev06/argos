package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"argos/server/internal/store"
)

// Rango del umbral propio de un grupo (segundos): alineado con el CHECK de la migracion 00007.
const (
	minGroupDownAfter = 30
	maxGroupDownAfter = 30 * 24 * 3600
)

type groupRefDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func toGroupRefDTO(g *store.GroupRef) *groupRefDTO {
	if g == nil {
		return nil
	}
	return &groupRefDTO{ID: g.ID, Name: g.Name}
}

type groupDTO struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	DownAfterSeconds *int   `json:"down_after_seconds"`
	AlwaysOn         bool   `json:"always_on"`
	HostCount        int    `json:"host_count"`
	// Retencion del detalle de metricas: el valor propio (null = hereda), el vigente y su origen (group o global).
	DetailRetentionDays *int          `json:"detail_retention_days"`
	EffectiveDetailDays int           `json:"effective_detail_days"`
	RetentionSource     string        `json:"retention_source"`
	Quotas              groupQuotaDTO `json:"quotas"`
}

// groupQuotaDTO son los limites propios del grupo (B7, "cliente" = grupo); null = sin limite propio (el tope
// global de la plataforma, si existe para ese recurso, sigue rigiendo igual). El consumo actual de cada uno
// (no solo hosts, que ya viene en HostCount) se pide aparte con GET /groups/{id}/usage.
type groupQuotaDTO struct {
	MaxHosts    *int `json:"max_hosts"`
	MaxDevices  *int `json:"max_devices"`
	MaxChecks   *int `json:"max_checks"`
	MaxUsers    *int `json:"max_users"`
	MaxRules    *int `json:"max_rules"`
	MaxChannels *int `json:"max_channels"`
}

func toGroupDTO(g store.Group) groupDTO {
	return groupDTO{ID: g.ID, Name: g.Name, DownAfterSeconds: g.DownAfterSeconds, AlwaysOn: g.AlwaysOn, HostCount: g.HostCount,
		DetailRetentionDays: g.DetailRetentionDays, EffectiveDetailDays: g.EffectiveDetailDays, RetentionSource: g.RetentionSource,
		Quotas: groupQuotaDTO{MaxHosts: g.MaxHosts, MaxDevices: g.MaxDevices, MaxChecks: g.MaxChecks, MaxUsers: g.MaxUsers, MaxRules: g.MaxRules, MaxChannels: g.MaxChannels}}
}

// groupQuotaDTO(usage) es el consumo actual de cada recurso frente a su cuota.
type groupUsageItemDTO struct {
	Current int  `json:"current"`
	Limit   *int `json:"limit"`
}

type groupUsageDTO struct {
	Hosts    groupUsageItemDTO `json:"hosts"`
	Devices  groupUsageItemDTO `json:"devices"`
	Checks   groupUsageItemDTO `json:"checks"`
	Users    groupUsageItemDTO `json:"users"`
	Rules    groupUsageItemDTO `json:"rules"`
	Channels groupUsageItemDTO `json:"channels"`
}

func toGroupUsageDTO(u store.GroupUsage) groupUsageDTO {
	conv := func(q store.GroupQuota) groupUsageItemDTO {
		return groupUsageItemDTO{Current: q.Current, Limit: q.Limit}
	}
	return groupUsageDTO{
		Hosts: conv(u.Hosts), Devices: conv(u.Devices), Checks: conv(u.Checks),
		Users: conv(u.Users), Rules: conv(u.Rules), Channels: conv(u.Channels),
	}
}

// groupQuotaDetails arma details de group_quota_exceeded: {"resource","current","limit"}.
func groupQuotaDetails(err error) any {
	var qe *store.GroupQuotaError
	if !errors.As(err, &qe) {
		return nil
	}
	return map[string]any{"resource": string(qe.Resource), "current": qe.Current, "limit": qe.Limit}
}

// getGroupUsage: GET /api/v1/groups/{id}/usage (admin) — consumo actual de los 6 recursos con cuota (B7).
func (a *API) getGroupUsage(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	u, err := a.store.GroupUsageByID(r.Context(), id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, toGroupUsageDTO(u))
	return nil
}

func (a *API) listGroups(w http.ResponseWriter, r *http.Request) error {
	groups, ungrouped, err := a.store.ListGroups(r.Context(), currentUser(r).Scope)
	if err != nil {
		return err
	}
	items := make([]groupDTO, 0, len(groups))
	for _, g := range groups {
		items = append(items, toGroupDTO(g))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":     items,
		"ungrouped": map[string]any{"host_count": ungrouped},
		"defaults":  map[string]any{"down_after_seconds": int(a.opts.HostDownAfter.Seconds())},
	})
	return nil
}

// checkDownAfter valida el umbral propio de un grupo (nil = usa el global).
func checkDownAfter(v *int) error {
	if v != nil && (*v < minGroupDownAfter || *v > maxGroupDownAfter) {
		return badRequest("down_after_seconds debe estar entre %d y %d", minGroupDownAfter, maxGroupDownAfter)
	}
	return nil
}

type createGroupReq struct {
	Name             string `json:"name"`
	DownAfterSeconds *int   `json:"down_after_seconds"`
	AlwaysOn         bool   `json:"always_on"`
	// DetailRetentionDays: dias de detalle propios del grupo (8..730); omitido o null = hereda la global.
	DetailRetentionDays *int `json:"detail_retention_days"`
}

func (a *API) createGroup(w http.ResponseWriter, r *http.Request) error {
	var req createGroupReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	name, err := checkName("name", req.Name, maxGroupNameLen)
	if err != nil {
		return err
	}
	if err := checkDownAfter(req.DownAfterSeconds); err != nil {
		return err
	}
	if err := checkDetailDays(req.DetailRetentionDays); err != nil {
		return err
	}
	g, err := a.store.CreateGroup(r.Context(), name, req.DownAfterSeconds, req.AlwaysOn, req.DetailRetentionDays)
	if err != nil {
		return err
	}

	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evGroupCreated, store.OutcomeOK, "group", g.ID
	a.audit(r, e, map[string]any{"name": g.Name, "always_on": g.AlwaysOn, "down_after_seconds": g.DownAfterSeconds})
	if g.DetailRetentionDays != nil {
		a.auditRetention(r, store.TargetGroup, g.ID, g.DetailRetentionDays)
	}

	writeJSON(w, http.StatusCreated, map[string]any{"group": toGroupDTO(g)})
	return nil
}

type updateGroupReq struct {
	Name             *string       `json:"name"`
	DownAfterSeconds optional[int] `json:"down_after_seconds"`
	AlwaysOn         *bool         `json:"always_on"`
	// DetailRetentionDays: dias de detalle propios del grupo (8..730, null = heredar el global).
	DetailRetentionDays optional[int] `json:"detail_retention_days"`
	ConfirmShrink       bool          `json:"confirm_shrink"`
	// Cuotas del cliente (B7): cada una null = sin limite propio (>= 0; 0 = no se permite ninguno).
	MaxHosts    optional[int] `json:"max_hosts"`
	MaxDevices  optional[int] `json:"max_devices"`
	MaxChecks   optional[int] `json:"max_checks"`
	MaxUsers    optional[int] `json:"max_users"`
	MaxRules    optional[int] `json:"max_rules"`
	MaxChannels optional[int] `json:"max_channels"`
}

// checkQuota valida una cuota (nil = sin limite; si viene, debe ser >= 0).
func checkQuota(field string, v *int) error {
	if v != nil && *v < 0 {
		return badRequest("%s debe ser >= 0 (o null para sin límite)", field)
	}
	return nil
}

func (a *API) updateGroup(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	var req updateGroupReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	anyQuotaSet := req.MaxHosts.Set || req.MaxDevices.Set || req.MaxChecks.Set || req.MaxUsers.Set || req.MaxRules.Set || req.MaxChannels.Set
	if req.Name == nil && !req.DownAfterSeconds.Set && req.AlwaysOn == nil && !req.DetailRetentionDays.Set && !anyQuotaSet {
		return badRequest("nada que cambiar: envia name, down_after_seconds, always_on, detail_retention_days o una cuota (max_hosts, max_devices, max_checks, max_users, max_rules, max_channels)")
	}
	change := store.GroupChange{
		AlwaysOn: req.AlwaysOn, DownAfterSet: req.DownAfterSeconds.Set, DownAfterSeconds: req.DownAfterSeconds.Value,
		DetailSet: req.DetailRetentionDays.Set, DetailDays: req.DetailRetentionDays.Value,
		MaxHostsSet: req.MaxHosts.Set, MaxHosts: req.MaxHosts.Value,
		MaxDevicesSet: req.MaxDevices.Set, MaxDevices: req.MaxDevices.Value,
		MaxChecksSet: req.MaxChecks.Set, MaxChecks: req.MaxChecks.Value,
		MaxUsersSet: req.MaxUsers.Set, MaxUsers: req.MaxUsers.Value,
		MaxRulesSet: req.MaxRules.Set, MaxRules: req.MaxRules.Value,
		MaxChannelsSet: req.MaxChannels.Set, MaxChannels: req.MaxChannels.Value,
	}
	for _, q := range []struct {
		field string
		v     *int
		set   bool
	}{
		{"max_hosts", req.MaxHosts.Value, req.MaxHosts.Set}, {"max_devices", req.MaxDevices.Value, req.MaxDevices.Set},
		{"max_checks", req.MaxChecks.Value, req.MaxChecks.Set}, {"max_users", req.MaxUsers.Value, req.MaxUsers.Set},
		{"max_rules", req.MaxRules.Value, req.MaxRules.Set}, {"max_channels", req.MaxChannels.Value, req.MaxChannels.Set},
	} {
		if q.set {
			if err := checkQuota(q.field, q.v); err != nil {
				return err
			}
		}
	}
	detail := map[string]any{}
	if req.Name != nil {
		name, err := checkName("name", *req.Name, maxGroupNameLen)
		if err != nil {
			return err
		}
		change.Name = &name
		detail["name"] = name
	}
	if err := checkDownAfter(req.DownAfterSeconds.Value); err != nil {
		return err
	}
	if req.DownAfterSeconds.Set {
		detail["down_after_seconds"] = req.DownAfterSeconds.Value
	}
	if req.AlwaysOn != nil {
		detail["always_on"] = *req.AlwaysOn
	}
	for _, q := range []struct {
		key string
		set bool
		v   *int
	}{
		{"max_hosts", req.MaxHosts.Set, req.MaxHosts.Value}, {"max_devices", req.MaxDevices.Set, req.MaxDevices.Value},
		{"max_checks", req.MaxChecks.Set, req.MaxChecks.Value}, {"max_users", req.MaxUsers.Set, req.MaxUsers.Value},
		{"max_rules", req.MaxRules.Set, req.MaxRules.Value}, {"max_channels", req.MaxChannels.Set, req.MaxChannels.Value},
	} {
		if q.set {
			detail[q.key] = q.v
		}
	}
	if req.DetailRetentionDays.Set {
		if err := checkDetailDays(req.DetailRetentionDays.Value); err != nil {
			return err
		}
		if err := a.checkShrink(r, store.RetentionTarget{Kind: store.TargetGroup, ID: id}, req.DetailRetentionDays.Value, req.ConfirmShrink); err != nil {
			return err
		}
	}

	g, err := a.store.UpdateGroup(r.Context(), id, change)
	if err != nil {
		return err
	}
	if req.DetailRetentionDays.Set {
		a.auditRetention(r, store.TargetGroup, g.ID, req.DetailRetentionDays.Value)
	}

	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evGroupUpdated, store.OutcomeOK, "group", g.ID
	a.audit(r, e, detail)

	writeJSON(w, http.StatusOK, map[string]any{"group": toGroupDTO(g)})
	return nil
}

func (a *API) deleteGroup(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	if err := a.store.DeleteGroup(r.Context(), id); err != nil {
		return err
	}
	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evGroupDeleted, store.OutcomeOK, "group", id
	a.audit(r, e, nil)

	writeJSON(w, http.StatusOK, map[string]any{})
	return nil
}

type setHostGroupReq struct {
	GroupID optional[string] `json:"group_id"`
}

// setHostGroup asigna (o quita, con null) el grupo de un host. La respuesta trae el estado resultante: un host
// down u offline se reclasifica segun el always_on del grupo nuevo.
func (a *API) setHostGroup(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return invalidID()
	}
	var req setHostGroupReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	if !req.GroupID.Set {
		return badRequest("group_id requerido (null = sin grupo)")
	}
	if g := req.GroupID.Value; g != nil && !isUUID(*g) {
		return badRequest("group_id invalido")
	}
	res, err := a.store.SetHostGroup(r.Context(), id, req.GroupID.Value)
	if err != nil {
		return err
	}

	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evHostGroupSet, store.OutcomeOK, "host", id
	a.audit(r, e, map[string]any{"group_id": req.GroupID.Value})

	writeJSON(w, http.StatusOK, map[string]any{"group": toGroupRefDTO(res.Group), "status": res.Status})
	return nil
}

type renameHostReq struct {
	Hostname string `json:"hostname"`
}

// renameHost cambia el nombre con el que se muestra un host con agente (los dispositivos se renombran con
// PATCH /devices/{id}). El agente no lo pisa: el nombre lo fija el alta (token) y ahora tambien el admin.
func (a *API) renameHost(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return invalidID()
	}
	var req renameHostReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	name, err := checkName("hostname", req.Hostname, maxHostnameLen)
	if err != nil {
		return err
	}
	if err := a.store.RenameHost(r.Context(), strings.ToLower(id), name); err != nil {
		return err
	}
	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evHostRenamed, store.OutcomeOK, "host", id
	a.audit(r, e, map[string]any{"name": name})
	writeJSON(w, http.StatusOK, map[string]any{"hostname": name})
	return nil
}
