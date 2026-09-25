package httpapi

import (
	"net/http"

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
}

func toGroupDTO(g store.Group) groupDTO {
	return groupDTO{ID: g.ID, Name: g.Name, DownAfterSeconds: g.DownAfterSeconds, AlwaysOn: g.AlwaysOn, HostCount: g.HostCount}
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
	g, err := a.store.CreateGroup(r.Context(), name, req.DownAfterSeconds, req.AlwaysOn)
	if err != nil {
		return err
	}

	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evGroupCreated, store.OutcomeOK, "group", g.ID
	a.audit(r, e, map[string]any{"name": g.Name, "always_on": g.AlwaysOn, "down_after_seconds": g.DownAfterSeconds})

	writeJSON(w, http.StatusCreated, map[string]any{"group": toGroupDTO(g)})
	return nil
}

type updateGroupReq struct {
	Name             *string       `json:"name"`
	DownAfterSeconds optional[int] `json:"down_after_seconds"`
	AlwaysOn         *bool         `json:"always_on"`
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
	if req.Name == nil && !req.DownAfterSeconds.Set && req.AlwaysOn == nil {
		return badRequest("nada que cambiar: envia name, down_after_seconds o always_on")
	}
	change := store.GroupChange{AlwaysOn: req.AlwaysOn, DownAfterSet: req.DownAfterSeconds.Set, DownAfterSeconds: req.DownAfterSeconds.Value}
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

	g, err := a.store.UpdateGroup(r.Context(), id, change)
	if err != nil {
		return err
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
