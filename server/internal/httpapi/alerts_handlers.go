package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"argos/server/internal/store"
)

// Vocabularios de los filtros y de las reglas (alineados con los CHECK de la migracion 00015).
var (
	validAlertStates   = map[string]bool{"open": true, "acknowledged": true, "resolved": true}
	validAlertSeverity = map[string]bool{"info": true, "warn": true, "critical": true}
)

// --- DTOs ---

type hostRefDTO struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
}

type alertDTO struct {
	ID           string    `json:"id"`
	RuleID       *string   `json:"rule_id"`
	RuleName     string    `json:"rule_name"`
	Kind         string    `json:"kind"`
	Metric       *string   `json:"metric"`
	Threshold    *float64  `json:"threshold"`
	Severity     string    `json:"severity"`
	State        string    `json:"state"`
	Suppressed   bool      `json:"suppressed"`
	OpenedAt     time.Time `json:"opened_at"`
	TriggerValue *float64  `json:"trigger_value"`
	// Detail (exposed_port: hasta 5 proto/puerto; parches: conteos, fecha o SO) solo va con el permiso del dato (can_inventory / can_updates); sin el, null y queda el conteo en trigger_value.
	Detail        *string    `json:"detail"`
	PeakValue     *float64   `json:"peak_value"`
	Host          hostRefDTO `json:"host"`
	AckedBy       *string    `json:"acknowledged_by"`
	AckedAt       *time.Time `json:"acknowledged_at"`
	ResolvedAt    *time.Time `json:"resolved_at"`
	ResolvedBy    *string    `json:"resolved_by"`
	ResolveReason *string    `json:"resolve_reason"`
}

// alertDetailAllowed dice si el usuario puede ver alerts.detail: es un dato de inventario (exposed_port) o de
// parches (pending_updates, reboot_pending, os_eol) y exige el permiso de cada uno; sin el solo queda el conteo.
func alertDetailAllowed(kind string, u *sessionUser) bool {
	switch kind {
	case "exposed_port":
		return u.seesInventory()
	case "pending_updates", "reboot_pending", "os_eol":
		return u.seesUpdates()
	}
	return false
}

func toAlertDTO(a store.Alert, u *sessionUser) alertDTO {
	var detail *string
	if alertDetailAllowed(a.RuleKind, u) {
		detail = a.Detail
	}
	return alertDTO{
		ID: a.ID, RuleID: a.RuleID, RuleName: a.RuleName, Kind: a.RuleKind, Metric: a.Metric, Threshold: a.Threshold,
		Severity: a.Severity, State: a.State, Suppressed: a.Suppressed, OpenedAt: a.OpenedAt.UTC(),
		TriggerValue: round2(a.TriggerValue), Detail: detail, PeakValue: round2(a.PeakValue),
		Host:    hostRefDTO{ID: a.HostID, Hostname: a.Hostname},
		AckedBy: a.AcknowledgedByName, AckedAt: utcPtr(a.AcknowledgedAt),
		ResolvedAt: utcPtr(a.ResolvedAt), ResolvedBy: a.ResolvedByName, ResolveReason: a.ResolveReason,
	}
}

type deliveryDTO struct {
	ID          int64      `json:"id"`
	ChannelID   string     `json:"channel_id"`
	ChannelName string     `json:"channel_name"`
	Event       string     `json:"event"`
	Seq         int        `json:"seq"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	CreatedAt   time.Time  `json:"created_at"`
	SentAt      *time.Time `json:"sent_at"`
}

type hostEventDTO struct {
	ID         int64     `json:"id"`
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	At         time.Time `json:"at"`
	Source     string    `json:"source"`
}

func toHostEventDTOs(evs []store.HostEvent) []hostEventDTO {
	out := make([]hostEventDTO, 0, len(evs))
	for _, e := range evs {
		out = append(out, hostEventDTO{ID: e.ID, FromStatus: e.FromStatus, ToStatus: e.ToStatus, At: e.At.UTC(), Source: e.Source})
	}
	return out
}

// --- cursor keyset (opened_at|at, id) ---

// keysetPayload es lo que viaja (firmado) en el cursor opaco de alertas y eventos.
type keysetPayload struct {
	F string `json:"f"` // huella de la consulta
	T int64  `json:"t"` // microsegundos Unix
	I string `json:"i"` // id
}

// encodeKeyset codifica y firma (HMAC-SHA256) la posicion; el cliente no puede fabricar ni alterar cursores.
func (a *API) encodeKeyset(fp string, at time.Time, id string) string {
	payload, err := json.Marshal(keysetPayload{F: fp, T: at.UnixMicro(), I: id})
	if err != nil {
		return "" // struct de string y enteros: no deberia ocurrir
	}
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(a.sign(payload))
}

// decodeKeyset valida firma y huella. Cualquier fallo es errInvalidCursor.
func (a *API) decodeKeyset(raw, fp string) (keysetPayload, error) {
	if len(raw) > maxCursorLen {
		return keysetPayload{}, errInvalidCursor
	}
	body, sig, ok := strings.Cut(raw, ".")
	if !ok {
		return keysetPayload{}, errInvalidCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return keysetPayload{}, errInvalidCursor
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, a.sign(payload)) {
		return keysetPayload{}, errInvalidCursor
	}
	var p keysetPayload
	if err := json.Unmarshal(payload, &p); err != nil || p.F != fp {
		return keysetPayload{}, errInvalidCursor
	}
	return p, nil
}

func fingerprintOf(parts ...any) string {
	b, err := json.Marshal(parts)
	if err != nil {
		return "" // strings y listas: no deberia ocurrir
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// --- filtros ---

// parseEnumList lee un filtro repetido o separado por comas con vocabulario cerrado: unicos y ordenados.
func parseEnumList(v url.Values, name string, allowed map[string]bool) ([]string, error) {
	var out []string
	for _, raw := range v[name] {
		for item := range strings.SplitSeq(raw, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if !allowed[item] {
				return nil, invalidFilter("%s tiene un valor invalido: %q", name, truncRunes(item, 32))
			}
			if !slices.Contains(out, item) {
				out = append(out, item)
			}
		}
	}
	if len(out) > maxFilterValues {
		return nil, invalidFilter("maximo %d valores en %s", maxFilterValues, name)
	}
	slices.Sort(out)
	return out, nil
}

// parseUUIDList lee un filtro de ids repetido o separado por comas: unicos, en minusculas y ordenados.
func parseUUIDList(v url.Values, name string) ([]string, error) {
	var out []string
	for _, raw := range v[name] {
		for item := range strings.SplitSeq(raw, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if !isUUID(item) {
				return nil, invalidFilter("%s debe ser un id valido", name)
			}
			if item = strings.ToLower(item); !slices.Contains(out, item) {
				out = append(out, item)
			}
		}
	}
	if len(out) > maxFilterValues {
		return nil, invalidFilter("maximo %d valores en %s", maxFilterValues, name)
	}
	slices.Sort(out)
	return out, nil
}

// parseLimit lee ?limit= (1..store.MaxAlertListLimit, default store.DefaultAlertListLimit).
func parseLimit(v url.Values) (int, error) {
	raw := v.Get("limit")
	if raw == "" {
		return store.DefaultAlertListLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > store.MaxAlertListLimit {
		return 0, badRequest("limit debe ser un entero entre 1 y %d", store.MaxAlertListLimit)
	}
	return n, nil
}

// --- handlers ---

// listAlerts devuelve una pagina de alertas (viewer o admin): las mas recientes primero.
func (a *API) listAlerts(w http.ResponseWriter, r *http.Request) error {
	v := r.URL.Query()
	var f store.AlertFilter
	var err error
	if f.States, err = parseEnumList(v, "state", validAlertStates); err != nil {
		return err
	}
	if f.Severities, err = parseEnumList(v, "severity", validAlertSeverity); err != nil {
		return err
	}
	if f.HostIDs, err = parseUUIDList(v, "host_id"); err != nil {
		return err
	}
	if f.RuleIDs, err = parseUUIDList(v, "rule_id"); err != nil {
		return err
	}
	if f.Limit, err = parseLimit(v); err != nil {
		return err
	}
	scope := currentUser(r).Scope
	fp := fingerprintOf("alerts", f.States, f.Severities, f.HostIDs, f.RuleIDs, scope.Key())
	if raw := v.Get("cursor"); raw != "" {
		p, err := a.decodeKeyset(raw, fp)
		if err != nil {
			return err
		}
		if !isUUID(p.I) {
			return errInvalidCursor
		}
		f.After = &store.AlertCursor{OpenedAt: time.UnixMicro(p.T), ID: p.I}
	}

	page, err := a.store.ListAlerts(r.Context(), scope, f)
	if err != nil {
		return err
	}
	items := make([]alertDTO, 0, len(page.Items))
	for _, al := range page.Items {
		items = append(items, toAlertDTO(al, currentUser(r)))
	}
	var next *string
	if page.NextCursor != nil {
		c := a.encodeKeyset(fp, page.NextCursor.OpenedAt, page.NextCursor.ID)
		next = &c
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next, "total": page.Total})
	return nil
}

// getAlert devuelve una alerta con sus entregas (estado, sin contenido ni secretos) y los ultimos eventos del host.
func (a *API) getAlert(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	d, err := a.store.AlertByID(r.Context(), currentUser(r).Scope, id)
	if err != nil {
		return err
	}
	deliveries := make([]deliveryDTO, 0, len(d.Deliveries))
	for _, x := range d.Deliveries {
		if !currentUser(r).Scope.All() {
			break // los canales son de admin (comparten cliente): un ambito restringido no ve sus ids ni nombres
		}
		deliveries = append(deliveries, deliveryDTO{
			ID: x.ID, ChannelID: x.ChannelID, ChannelName: x.ChannelName, Event: x.Event, Seq: x.Seq, Status: x.Status,
			Attempts: x.Attempts, CreatedAt: x.CreatedAt.UTC(), SentAt: utcPtr(x.SentAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"alert": toAlertDTO(d.Alert, currentUser(r)), "deliveries": deliveries, "host_events": toHostEventDTOs(d.HostEvents),
	})
	return nil
}

// ackAlert reconoce una alerta: un admin o un viewer con can_ack (requireAck), siempre dentro de su ambito (fuera
// de el, el mismo 404 que una alerta inexistente).
func (a *API) ackAlert(w http.ResponseWriter, r *http.Request) error {
	scope := currentUser(r).Scope
	return a.alertAction(w, r, evAlertAcked, func(ctx context.Context, id, userID, username string) (store.Alert, error) {
		return a.store.AckAlert(ctx, scope, id, userID, username)
	})
}

// resolveAlert solo lo llama un admin (lo garantiza la ruta).
func (a *API) resolveAlert(w http.ResponseWriter, r *http.Request) error {
	return a.alertAction(w, r, evAlertResolved, a.store.ResolveAlert)
}

// alertAction es el cuerpo comun de ack y resolve (la ruta decide quien puede), auditado con el actor, id, regla,
// severidad y hostname (nada mas).
func (a *API) alertAction(w http.ResponseWriter, r *http.Request, event string,
	do func(ctx context.Context, id, userID, username string) (store.Alert, error),
) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return badRequest("id invalido")
	}
	u := currentUser(r)
	al, err := do(r.Context(), id, u.ID, u.Username)
	if err != nil {
		return err
	}
	e := actor(u)
	e.Event, e.Outcome, e.TargetType, e.TargetID = event, store.OutcomeOK, "alert", id
	a.audit(r, e, map[string]any{"name": al.RuleName, "severity": al.Severity, "hostname": al.Hostname})
	writeJSON(w, http.StatusOK, map[string]any{"alert": toAlertDTO(al, u)})
	return nil
}

type severityCountDTO struct {
	Open         int `json:"open"`
	Acknowledged int `json:"acknowledged"`
}

// alertSummary devuelve los contadores de alertas activas por severidad y estado.
func (a *API) alertSummary(w http.ResponseWriter, r *http.Request) error {
	sum, err := a.store.AlertSummary(r.Context(), currentUser(r).Scope)
	if err != nil {
		return err
	}
	by := make(map[string]severityCountDTO, len(sum.BySeverity))
	for sev, c := range sum.BySeverity {
		by[sev] = severityCountDTO{Open: c.Open, Acknowledged: c.Acknowledged}
	}
	writeJSON(w, http.StatusOK, map[string]any{"active": sum.Active, "suppressed": sum.Suppressed, "by_severity": by})
	return nil
}

// hostEvents devuelve las transiciones de status de un host, las mas recientes primero.
func (a *API) hostEvents(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return invalidID()
	}
	v := r.URL.Query()
	limit, err := parseLimit(v)
	if err != nil {
		return err
	}
	scope := currentUser(r).Scope
	fp := fingerprintOf("events", strings.ToLower(id), scope.Key())
	var after *store.EventCursor
	if raw := v.Get("cursor"); raw != "" {
		p, err := a.decodeKeyset(raw, fp)
		if err != nil {
			return err
		}
		evID, err := strconv.ParseInt(p.I, 10, 64)
		if err != nil || evID < 1 {
			return errInvalidCursor
		}
		after = &store.EventCursor{At: time.UnixMicro(p.T), ID: evID}
	}
	page, err := a.store.ListHostEvents(r.Context(), scope, id, after, limit)
	if err != nil {
		return err
	}
	var next *string
	if page.NextCursor != nil {
		c := a.encodeKeyset(fp, page.NextCursor.At, strconv.FormatInt(page.NextCursor.ID, 10))
		next = &c
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": toHostEventDTOs(page.Items), "next_cursor": next})
	return nil
}
