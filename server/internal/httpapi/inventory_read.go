package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"argos/server/internal/store"
)

// Estado de un modulo del inventario en las respuestas de lectura.
const (
	invStatusOK       = "ok"       // hay foto
	invStatusNoData   = "no_data"  // el host aun no ha reportado inventario alguno
	invStatusDisabled = "disabled" // el host reporta otros modulos pero nunca este: apagado en el agente
	invAuditWindow    = 5 * time.Minute
	invAuditModChange = "changes" // valor de detail.module al leer el historial
	invAuditModAll    = "overview"
)

var (
	inventoryModules = []string{store.InventoryPorts, store.InventoryServices, store.InventoryProcesses}
	validChangeKinds = map[string]bool{"added": true, "removed": true, "changed": true, "overflow": true}
	validChangeMods  = map[string]bool{store.InventoryPorts: true, store.InventoryServices: true}
)

type inventoryModuleDTO struct {
	Status      string     `json:"status"`
	Count       int        `json:"count"`
	Truncated   bool       `json:"truncated"`
	CollectedAt *time.Time `json:"collected_at"`
	UpdatedAt   *time.Time `json:"updated_at"`
}

type inventoryListDTO struct {
	Module      string     `json:"module"`
	Status      string     `json:"status"`
	Count       int        `json:"count"`
	Truncated   bool       `json:"truncated"`
	CollectedAt *time.Time `json:"collected_at"`
	UpdatedAt   *time.Time `json:"updated_at"`
	Items       any        `json:"items"`
}

type inventoryChangeDTO struct {
	ID     int64           `json:"id"`
	Module string          `json:"module"`
	Kind   string          `json:"kind"`
	Key    string          `json:"key"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
	At     time.Time       `json:"at"`
}

// moduleStatuses clasifica cada modulo segun lo que el host ha reportado.
func moduleStatuses(metas []store.InventoryMeta) map[string]inventoryModuleDTO {
	byMod := make(map[string]store.InventoryMeta, len(metas))
	for _, m := range metas {
		byMod[m.Module] = m
	}
	out := make(map[string]inventoryModuleDTO, len(inventoryModules))
	for _, mod := range inventoryModules {
		m, ok := byMod[mod]
		switch {
		case ok:
			c, u := m.CollectedAt.UTC(), m.ReceivedAt.UTC()
			out[mod] = inventoryModuleDTO{Status: invStatusOK, Count: m.Count, Truncated: m.Truncated, CollectedAt: &c, UpdatedAt: &u}
		case len(metas) == 0:
			out[mod] = inventoryModuleDTO{Status: invStatusNoData}
		default:
			out[mod] = inventoryModuleDTO{Status: invStatusDisabled}
		}
	}
	return out
}

// auditInventoryRead registra la lectura (actor, host y modulo; nunca contenido), una vez por
// (usuario, host, modulo) cada invAuditWindow pa que un refresco del frontend no inunde audit_log.
func (a *API) auditInventoryRead(r *http.Request, u *sessionUser, hostID, module string) {
	if ok, _ := a.inventoryReadDedup.allow(u.ID+"|"+hostID+"|"+module, a.now()); !ok {
		return
	}
	e := actor(u)
	e.Event, e.Outcome, e.TargetType, e.TargetID = evInventoryRead, store.OutcomeOK, "host", hostID
	a.audit(r, e, map[string]any{"module": module})
}

// inventoryOverview resume los modulos del host: estado, conteos, frescura y truncado.
func (a *API) inventoryOverview(w http.ResponseWriter, r *http.Request) error {
	u, id := currentUser(r), strings.ToLower(r.PathValue("id"))
	metas, err := a.store.InventoryOverview(r.Context(), u.Scope, id)
	if err != nil {
		return err
	}
	a.auditInventoryRead(r, u, id, invAuditModAll)
	writeJSON(w, http.StatusOK, map[string]any{"modules": moduleStatuses(metas)})
	return nil
}

// inventoryModule devuelve la lista completa (acotada por el ingest) de un modulo.
func (a *API) inventoryModule(module string) handlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		u, id := currentUser(r), strings.ToLower(r.PathValue("id"))
		metas, err := a.store.InventoryOverview(r.Context(), u.Scope, id)
		if err != nil {
			return err
		}
		st := moduleStatuses(metas)[module]
		dto := inventoryListDTO{Module: module, Status: st.Status, Count: st.Count, Truncated: st.Truncated,
			CollectedAt: st.CollectedAt, UpdatedAt: st.UpdatedAt, Items: []struct{}{}}
		if st.Status == invStatusOK {
			raw, err := a.store.InventoryItems(r.Context(), u.Scope, id, module)
			if err != nil {
				return err
			}
			if dto.Items, err = decodeInventoryItems(module, raw); err != nil {
				return err
			}
		}
		a.auditInventoryRead(r, u, id, module)
		writeJSON(w, http.StatusOK, dto)
		return nil
	}
}

// decodeInventoryItems pasa los elementos guardados por el DTO tipado de su modulo: solo salen los campos de
// la lista blanca aunque la fila tuviera otros.
func decodeInventoryItems(module string, raw []json.RawMessage) (any, error) {
	var decode func(json.RawMessage) (any, error)
	switch module {
	case store.InventoryPorts:
		decode = func(b json.RawMessage) (any, error) { var it portItem; return it, json.Unmarshal(b, &it) }
	case store.InventoryServices:
		decode = func(b json.RawMessage) (any, error) { var it serviceItem; return it, json.Unmarshal(b, &it) }
	default:
		decode = func(b json.RawMessage) (any, error) { var it processItem; return it, json.Unmarshal(b, &it) }
	}
	out := make([]any, 0, len(raw))
	for _, b := range raw {
		it, err := decode(b)
		if err != nil {
			return nil, fmt.Errorf("decodificando elemento de %s: %w", module, err)
		}
		out = append(out, it)
	}
	return out, nil
}

// inventoryChanges pagina el historial de cambios de puertos y servicios (?module=&kind=&cursor=&limit=).
func (a *API) inventoryChanges(w http.ResponseWriter, r *http.Request) error {
	u, id := currentUser(r), strings.ToLower(r.PathValue("id"))
	v := r.URL.Query()
	module, kind := v.Get("module"), v.Get("kind")
	if module != "" && !validChangeMods[module] {
		return invalidFilter("module debe ser ports o services")
	}
	if kind != "" && !validChangeKinds[kind] {
		return invalidFilter("kind debe ser added, removed, changed u overflow")
	}
	limit, err := parseLimit(v)
	if err != nil {
		return err
	}
	fp := fingerprintOf("inventory_changes", id, u.Scope.Key(), module, kind)
	var after *store.EventCursor
	if raw := v.Get("cursor"); raw != "" {
		p, err := a.decodeKeyset(raw, fp)
		if err != nil {
			return err
		}
		cid, err := strconv.ParseInt(p.I, 10, 64)
		if err != nil || cid < 1 {
			return errInvalidCursor
		}
		after = &store.EventCursor{At: time.UnixMicro(p.T), ID: cid}
	}
	page, err := a.store.ListInventoryChanges(r.Context(), u.Scope, id, module, kind, after, limit)
	if err != nil {
		return err
	}
	items := make([]inventoryChangeDTO, 0, len(page.Items))
	for _, c := range page.Items {
		items = append(items, inventoryChangeDTO{ID: c.ID, Module: c.Module, Kind: c.Kind, Key: c.Key,
			Before: c.Before, After: c.After, At: c.At.UTC()})
	}
	var next *string
	if page.NextCursor != nil {
		c := a.encodeKeyset(fp, page.NextCursor.At, strconv.FormatInt(page.NextCursor.ID, 10))
		next = &c
	}
	a.auditInventoryRead(r, u, id, invAuditModChange)
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
	return nil
}
