package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"argos/server/internal/store"
)

const maxBulkIDs = 200

// Largo maximo de una etiqueta (alineado con el CHECK de la migracion 00016).
const maxTagLen = 32

// invalidTag es un 400 invalid_tag: etiqueta con formato invalido.
func invalidTag(format string, args ...any) error {
	return &clientError{status: http.StatusBadRequest, code: "invalid_tag", msg: fmt.Sprintf(format, args...)}
}

// isBidiControl reporta los caracteres de control bidireccional (los rechaza tambien el CHECK de la BD).
func isBidiControl(r rune) bool {
	return r == '\u200e' || r == '\u200f' || (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069')
}

// checkTag valida y recorta una etiqueta: 1..32 caracteres, sin control ni bidi.
func checkTag(raw string) (string, error) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return "", invalidTag("etiqueta vacia")
	}
	if utf8.RuneCountInString(t) > maxTagLen {
		return "", invalidTag("etiqueta excede %d caracteres", maxTagLen)
	}
	if hasControl(t) || strings.IndexFunc(t, isBidiControl) >= 0 {
		return "", invalidTag("etiqueta contiene caracteres invalidos")
	}
	return t, nil
}

type setTagsReq struct {
	// Puntero: distingue "tags ausente" (400) de una lista vacia (quita todas).
	Tags *[]string `json:"tags"`
}

// setHostTags reemplaza todas las etiquetas del host.
func (a *API) setHostTags(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return invalidID()
	}
	var req setTagsReq
	if err := decodeJSON(w, r, &req, maxSmallBodyBytes, true); err != nil {
		return err
	}
	if req.Tags == nil {
		return badRequest("tags requerido (lista vacia = quitar todas)")
	}
	// Se valida el largo de la lista pedida antes de recorrer (evita trabajo con listas absurdas).
	if len(*req.Tags) > 10*store.MaxTagsPerHost {
		return badRequest("tags excede el maximo permitido")
	}
	seen := make(map[string]bool, len(*req.Tags))
	tags := make([]string, 0, len(*req.Tags))
	for _, raw := range *req.Tags {
		t, err := checkTag(raw)
		if err != nil {
			return err
		}
		if l := strings.ToLower(t); !seen[l] {
			seen[l] = true
			tags = append(tags, t)
		}
	}
	if len(tags) > store.MaxTagsPerHost {
		return store.ErrTooManyTags
	}

	saved, err := a.store.SetHostTags(r.Context(), id, tags)
	if err != nil {
		return err
	}

	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType, e.TargetID = evHostTagsSet, store.OutcomeOK, "host", id
	a.audit(r, e, map[string]any{"count": len(saved)})

	writeJSON(w, http.StatusOK, map[string]any{"tags": saved})
	return nil
}

// listTags devuelve las etiquetas en uso con su conteo de hosts (acotado; pa sugerencias y filtro).
func (a *API) listTags(w http.ResponseWriter, r *http.Request) error {
	tags, err := a.store.ListTags(r.Context())
	if err != nil {
		return err
	}
	type tagDTO struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	out := make([]tagDTO, 0, len(tags))
	for _, t := range tags {
		out = append(out, tagDTO{Name: t.Name, Count: t.Count})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": out})
	return nil
}

type bulkReq struct {
	IDs     []string         `json:"ids"`
	Action  string           `json:"action"`
	GroupID optional[string] `json:"group_id"`
	Tag     *string          `json:"tag"`
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
		if req.Tag != nil {
			return store.Bulk{}, nil, badRequest("tag no aplica a set_group")
		}
		if !req.GroupID.Set {
			return store.Bulk{}, nil, badRequest("group_id requerido (null = sin grupo)")
		}
		if g := req.GroupID.Value; g != nil && !isUUID(*g) {
			return store.Bulk{}, nil, badRequest("group_id invalido")
		}
		b.GroupID = req.GroupID.Value
		detail["group_id"] = req.GroupID.Value
	case store.BulkAddTag, store.BulkRemoveTag:
		if req.GroupID.Set {
			return store.Bulk{}, nil, badRequest("group_id no aplica a %s", req.Action)
		}
		if req.Tag == nil {
			return store.Bulk{}, nil, badRequest("tag requerido")
		}
		t, err := checkTag(*req.Tag)
		if err != nil {
			return store.Bulk{}, nil, err
		}
		b.Tag = t
		detail["tag"] = t
	case store.BulkDisable:
		if req.GroupID.Set || req.Tag != nil {
			return store.Bulk{}, nil, badRequest("group_id y tag no aplican a disable")
		}
	default:
		return store.Bulk{}, nil, badRequest("action debe ser set_group, add_tag, remove_tag o disable")
	}
	return b, detail, nil
}
