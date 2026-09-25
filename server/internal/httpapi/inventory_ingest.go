package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"regexp"
	"time"
	"unicode"
	"unicode/utf8"

	"argos/server/internal/store"
)

// Limites del inventario (los mismos topes que aplica el agente).
const (
	maxInventoryBytes    = 256 << 10
	maxInventoryPorts    = 512
	maxInventoryServices = 1024
	maxInventoryProcs    = 100
	maxInvNameLen        = 64
	maxInvDisplayLen     = 128
	maxInvProcCount      = 10_000_000
	maxInvRSSBytes       = 1 << 50
)

var (
	invRevRE         = regexp.MustCompile(`^[0-9a-f]{64}$`)
	validServiceRun  = map[string]bool{"running": true, "stopped": true, "paused": true, "other": true}
	validServiceInit = map[string]bool{"auto": true, "manual": true, "disabled": true, "other": true}
)

// inventoryReq es el cuerpo de POST /agent/inventory. Un latido lleva solo module y rev; un envio completo lleva
// ademas collected_at e items. No hay host_id: el host es siempre el del token.
type inventoryReq struct {
	Module      string          `json:"module"`
	Rev         string          `json:"rev"`
	Items       json.RawMessage `json:"items"`
	Truncated   bool            `json:"truncated"`
	CollectedAt *time.Time      `json:"collected_at"`
}

// Elementos tal como se guardan (lista blanca). El pid llega del agente pero no se guarda.
type portItem struct {
	Proto      string `json:"proto"`
	Bind       string `json:"bind"`
	Port       int    `json:"port"`
	Proc       string `json:"proc"`
	OwnerKnown bool   `json:"owner_known"`
}

type serviceItem struct {
	Name    string `json:"name"`
	Display string `json:"display"`
	State   string `json:"state"`
	Start   string `json:"start"`
}

type processItem struct {
	Name     string `json:"name"`
	Count    int    `json:"count"`
	RSSBytes int64  `json:"rss_bytes"`
}

type portWire struct {
	portItem
	PID int32 `json:"pid"`
}

func (a *API) agentInventory(w http.ResponseWriter, r *http.Request) error {
	hostID, err := a.authenticateAgent(r)
	if err != nil {
		return err
	}
	if ok, wait := a.inventoryLimiter.allow(hostID, a.now()); !ok {
		return rateLimited(wait)
	}
	var req inventoryReq
	if err := decodeJSON(w, r, &req, maxInventoryBytes, true); err != nil {
		return err
	}
	if !store.ValidInventoryModule(req.Module) {
		return badRequest("module invalido")
	}
	if !invRevRE.MatchString(req.Rev) {
		return badRequest("rev invalido")
	}
	now := a.now()

	if req.CollectedAt == nil { // latido
		if len(req.Items) != 0 || req.Truncated {
			return badRequest("un latido no lleva items ni truncated")
		}
		if err := a.store.InventoryHeartbeat(r.Context(), hostID, req.Module, req.Rev, now); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}

	if at := *req.CollectedAt; at.Before(now.Add(-maxTimestampAge)) || at.After(now.Add(maxTimestampSkew)) {
		return badRequest("collected_at fuera de rango")
	}
	if req.Module == store.InventoryUpdates {
		// El permiso can_updates aplica a la lectura, no a este envio: el agente solo escribe su propio host.
		snap, err := validateUpdates(req.Items, req.Rev, req.Truncated, *req.CollectedAt, now)
		if err != nil {
			return err
		}
		if err := a.store.IngestUpdates(r.Context(), hostID, snap, now); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	items, err := validateInventoryItems(req.Module, req.Items)
	if err != nil {
		return err
	}
	err = a.store.IngestInventory(r.Context(), hostID, store.InventoryFull{
		Module: req.Module, Rev: req.Rev, Items: items, Truncated: req.Truncated, CollectedAt: *req.CollectedAt,
	}, now)
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// validateInventoryItems decodifica con lista blanca estricta (un campo fuera de ella, como cmdline o exe, es 400) y
// valida cada elemento. Devuelve los elementos ya normalizados, sin el pid. Las claves repetidas se descartan
// (puertos con SO_REUSEPORT, nombres de servicio que chocan al recortarse).
func validateInventoryItems(module string, raw json.RawMessage) ([]json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, badRequest("items requerido")
	}
	var out []any
	var err error
	switch module {
	case store.InventoryPorts:
		out, err = validatePorts(raw)
	case store.InventoryServices:
		out, err = validateServiceItems(raw)
	default:
		out, err = validateProcessItems(raw)
	}
	if err != nil {
		return nil, err
	}
	items := make([]json.RawMessage, len(out))
	for i, it := range out {
		b, err := json.Marshal(it)
		if err != nil {
			return nil, fmt.Errorf("serializando item de inventario: %w", err)
		}
		items[i] = b
	}
	return items, nil
}

// decodeItems lee un arreglo JSON rechazando campos desconocidos y contenido sobrante. `null` es una lista vacia.
func decodeItems[T any](raw json.RawMessage, limit int) ([]T, error) {
	var in []T
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return nil, badRequest("items invalido")
	}
	if len(in) > limit {
		return nil, badRequest("maximo %d items", limit)
	}
	return in, nil
}

func validatePorts(raw json.RawMessage) ([]any, error) {
	in, err := decodeItems[portWire](raw, maxInventoryPorts)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(in))
	out := make([]any, 0, len(in))
	for i, p := range in {
		if p.Proto != "tcp" && p.Proto != "udp" {
			return nil, badRequest("items[%d].proto invalido", i)
		}
		addr, err := netip.ParseAddr(p.Bind)
		if err != nil || addr.Zone() != "" {
			return nil, badRequest("items[%d].bind invalido", i)
		}
		if p.Port < 1 || p.Port > 65535 {
			return nil, badRequest("items[%d].port fuera de rango", i)
		}
		if p.PID < 0 {
			return nil, badRequest("items[%d].pid invalido", i)
		}
		if p.OwnerKnown {
			if err := invText("proc", i, p.Proc, maxInvNameLen, true); err != nil {
				return nil, err
			}
		} else {
			p.Proc = ""
		}
		p.Bind = addr.String()
		key := fmt.Sprintf("%s|%s|%d", p.Proto, p.Bind, p.Port)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p.portItem)
	}
	return out, nil
}

func validateServiceItems(raw json.RawMessage) ([]any, error) {
	in, err := decodeItems[serviceItem](raw, maxInventoryServices)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(in))
	out := make([]any, 0, len(in))
	for i, s := range in {
		if err := invText("name", i, s.Name, maxInvNameLen, true); err != nil {
			return nil, err
		}
		if err := invText("display", i, s.Display, maxInvDisplayLen, false); err != nil {
			return nil, err
		}
		if !validServiceRun[s.State] {
			return nil, badRequest("items[%d].state invalido", i)
		}
		if !validServiceInit[s.Start] {
			return nil, badRequest("items[%d].start invalido", i)
		}
		if seen[s.Name] {
			continue
		}
		seen[s.Name] = true
		out = append(out, s)
	}
	return out, nil
}

func validateProcessItems(raw json.RawMessage) ([]any, error) {
	in, err := decodeItems[processItem](raw, maxInventoryProcs)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(in))
	out := make([]any, 0, len(in))
	for i, p := range in {
		if err := invText("name", i, p.Name, maxInvNameLen, true); err != nil {
			return nil, err
		}
		if p.Count < 1 || p.Count > maxInvProcCount {
			return nil, badRequest("items[%d].count fuera de rango", i)
		}
		if p.RSSBytes < 0 || p.RSSBytes > maxInvRSSBytes {
			return nil, badRequest("items[%d].rss_bytes fuera de rango", i)
		}
		if seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		out = append(out, p)
	}
	return out, nil
}

// invText valida un texto de inventario: UTF-8 valido, sin caracteres de control, hasta max runas.
func invText(field string, i int, s string, limit int, required bool) error {
	if s == "" {
		if required {
			return badRequest("items[%d].%s requerido", i, field)
		}
		return nil
	}
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > limit {
		return badRequest("items[%d].%s invalido", i, field)
	}
	for _, r := range s {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return badRequest("items[%d].%s contiene caracteres invalidos", i, field)
		}
	}
	return nil
}
