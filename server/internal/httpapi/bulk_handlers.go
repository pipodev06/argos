package httpapi

import (
	"net/http"

	"argos/server/internal/store"
)

const maxBulkIDs = 200

type bulkReq struct {
	IDs     []string         `json:"ids"`
	Action  string           `json:"action"`
	GroupID optional[string] `json:"group_id"`
}

// bulkHosts aplica una accion a hasta maxBulkIDs hosts en una transaccion (todo o nada). Responde con
// cuantos hosts cambiaron de verdad.
func (a *API) bulkHosts(w http.ResponseWriter, r *http.Request) error {
	var req bulkReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes*4, true); err != nil {
		return err
	}
	b, detail, err := validateBulk(req)
	if err != nil {
		return err
	}
	affected, err := a.store.BulkHosts(r.Context(), b)
	if err != nil {
		return err
	}

	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType = evHostsBulk, store.OutcomeOK, "host"
	a.audit(r, e, detail)

	writeJSON(w, http.StatusOK, map[string]any{"affected": affected})
	return nil
}

// validateBulk valida la solicitud y arma la accion del store y el detalle de auditoria. Rechaza los campos
// que no aplican a la accion (un typo no debe convertirse en otra accion silenciosamente).
func validateBulk(req bulkReq) (store.Bulk, map[string]any, error) {
	if len(req.IDs) == 0 || len(req.IDs) > maxBulkIDs {
		return store.Bulk{}, nil, badRequest("ids debe tener entre 1 y %d hosts", maxBulkIDs)
	}
	seen := make(map[string]bool, len(req.IDs))
	ids := make([]string, 0, len(req.IDs))
	for _, id := range req.IDs {
		if !isUUID(id) {
			return store.Bulk{}, nil, badRequest("id invalido en ids")
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	b := store.Bulk{IDs: ids, Action: req.Action}
	detail := map[string]any{"action": req.Action, "count": len(ids)}

	switch req.Action {
	case store.BulkSetGroup:
		if !req.GroupID.Set {
			return store.Bulk{}, nil, badRequest("group_id requerido (null = sin grupo)")
		}
		if g := req.GroupID.Value; g != nil && !isUUID(*g) {
			return store.Bulk{}, nil, badRequest("group_id invalido")
		}
		b.GroupID = req.GroupID.Value
		detail["group_id"] = req.GroupID.Value
	case store.BulkDisable:
		if req.GroupID.Set {
			return store.Bulk{}, nil, badRequest("group_id no aplica a disable")
		}
	default:
		return store.Bulk{}, nil, badRequest("action debe ser set_group o disable")
	}
	return b, detail, nil
}
