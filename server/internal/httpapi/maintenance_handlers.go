package httpapi

import (
	"net/http"
	"time"

	"argos/server/internal/store"
)

// Limites de una ventana de mantenimiento: alineados con los CHECK de la migracion 00015 (tope de 31 dias,
// alcance `all` hasta 24 h) mas dos guardas de cordura sobre las fechas (no absurdas).
const (
	maxWindowSpan       = 31 * 24 * time.Hour
	maxWindowSpanAll    = 24 * time.Hour
	maxWindowPastStart  = time.Hour
	maxWindowFutureLead = 90 * 24 * time.Hour
)

type maintenanceDTO struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
	Scope     scopeDTO  `json:"scope"`
	Status    string    `json:"status"`
	CreatedBy *string   `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

func toMaintenanceDTO(w store.MaintenanceWindow) maintenanceDTO {
	return maintenanceDTO{
		ID: w.ID, Name: w.Name, StartsAt: w.StartsAt.UTC(), EndsAt: w.EndsAt.UTC(), Scope: toScopeDTO(w.Scope, w.GroupName),
		Status: w.Status, CreatedBy: w.CreatedByName, CreatedAt: w.CreatedAt.UTC(),
	}
}

// validateWindow comprueba una ventana completa. checkDates exige ademas que no haya terminado ya y que el inicio
// no sea absurdo (solo al crear o al cambiar las fechas: renombrar una ventana vieja no debe fallar por eso).
func validateWindow(w *store.MaintenanceWindow, now time.Time, checkDates bool) error {
	name, err := checkName("name", w.Name, maxRuleNameLen)
	if err != nil {
		return err
	}
	w.Name = name
	if w.StartsAt.IsZero() || w.EndsAt.IsZero() {
		return badRequest("starts_at y ends_at requeridos (RFC 3339)")
	}
	span := w.EndsAt.Sub(w.StartsAt)
	switch {
	case span <= 0:
		return badRequest("ends_at debe ser posterior a starts_at")
	case span > maxWindowSpan:
		return badRequest("la ventana no puede durar mas de 31 dias")
	case w.Scope.Type == store.ScopeAll && span > maxWindowSpanAll:
		return badRequest("una ventana sobre todos los hosts no puede durar mas de 24 horas: acota el alcance a un grupo o etiqueta")
	}
	if checkDates {
		switch {
		case !w.EndsAt.After(now):
			return badRequest("ends_at ya paso")
		case w.StartsAt.Before(now.Add(-maxWindowPastStart)):
			return badRequest("starts_at no puede ser anterior a 1 hora atras")
		case w.StartsAt.After(now.Add(maxWindowFutureLead)):
			return badRequest("starts_at no puede ser mas de 90 dias en el futuro")
		}
	}
	return nil
}

func maintenanceAuditDetail(w store.MaintenanceWindow) map[string]any {
	d := map[string]any{
		"name": w.Name, "scope_type": w.Scope.Type,
		"starts_at": w.StartsAt.UTC().Format(time.RFC3339), "ends_at": w.EndsAt.UTC().Format(time.RFC3339),
	}
	if w.Scope.GroupID != nil {
		d["group_id"] = *w.Scope.GroupID
	}
	if w.Scope.Tag != nil {
		d["tag"] = *w.Scope.Tag
	}
	return d
}

func (a *API) listMaintenance(w http.ResponseWriter, r *http.Request) error {
	ws, err := a.store.ListMaintenance(r.Context())
	if err != nil {
		return err
	}
	items := make([]maintenanceDTO, 0, len(ws))
	for _, x := range ws {
		items = append(items, toMaintenanceDTO(x))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

func (a *API) getMaintenance(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	win, err := a.store.MaintenanceByID(r.Context(), id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"maintenance": toMaintenanceDTO(win)})
	return nil
}

type createMaintenanceReq struct {
	Name     string    `json:"name"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	Scope    *scopeReq `json:"scope"`
}

func (a *API) createMaintenance(w http.ResponseWriter, r *http.Request) error {
	var req createMaintenanceReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	if req.Scope == nil {
		return badRequest("scope requerido (el alcance es explicito: all, group o tag)")
	}
	scope, err := req.Scope.toScope()
	if err != nil {
		return err
	}
	win := store.MaintenanceWindow{Name: req.Name, StartsAt: req.StartsAt, EndsAt: req.EndsAt, Scope: scope}
	if err := validateWindow(&win, a.now(), true); err != nil {
		return err
	}

	u := currentUser(r)
	created, err := a.store.CreateMaintenance(r.Context(), win, u.ID, u.Username)
	if err != nil {
		return err
	}
	e := actor(u)
	e.Event, e.Outcome, e.TargetType, e.TargetID = evMaintenanceCreated, store.OutcomeOK, "maintenance", created.ID
	a.audit(r, e, maintenanceAuditDetail(created))

	writeJSON(w, http.StatusCreated, map[string]any{"maintenance": toMaintenanceDTO(created)})
	return nil
}

type updateMaintenanceReq struct {
	Name     *string    `json:"name"`
	StartsAt *time.Time `json:"starts_at"`
	EndsAt   *time.Time `json:"ends_at"`
	Scope    *scopeReq  `json:"scope"`
}

func (a *API) updateMaintenance(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	var req updateMaintenanceReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	if req.Name == nil && req.StartsAt == nil && req.EndsAt == nil && req.Scope == nil {
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
	datesChanged := req.StartsAt != nil || req.EndsAt != nil

	updated, err := a.store.UpdateMaintenance(r.Context(), id, func(win *store.MaintenanceWindow) error {
		if req.Name != nil {
			win.Name = *req.Name
		}
		if req.StartsAt != nil {
			win.StartsAt = *req.StartsAt
		}
		if req.EndsAt != nil {
			win.EndsAt = *req.EndsAt
		}
		if newScope != nil {
			win.Scope = *newScope
		}
		return validateWindow(win, a.now(), datesChanged)
	})
	if err != nil {
		return err
	}
	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evMaintenanceUpdated, store.OutcomeOK, "maintenance", updated.ID
	a.audit(r, e, maintenanceAuditDetail(updated))

	writeJSON(w, http.StatusOK, map[string]any{"maintenance": toMaintenanceDTO(updated)})
	return nil
}

func (a *API) deleteMaintenance(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	name, err := a.store.DeleteMaintenance(r.Context(), id)
	if err != nil {
		return err
	}
	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evMaintenanceDeleted, store.OutcomeOK, "maintenance", id
	a.audit(r, e, map[string]any{"name": name})
	writeJSON(w, http.StatusOK, map[string]any{})
	return nil
}
