package httpapi

import (
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
	"unicode/utf8"

	"argos/server/internal/store"
)

// maxAuditRange es el rango maximo entre from y to.
const maxAuditRange = 366 * 24 * time.Hour

// validOutcomes es el vocabulario de audit_log.outcome (CHECK de la migracion 00004).
var validOutcomes = map[string]bool{store.OutcomeOK: true, store.OutcomeFail: true, store.OutcomeDenied: true}

type auditItemDTO struct {
	ID            int64           `json:"id"`
	TS            time.Time       `json:"ts"`
	Event         string          `json:"event"`
	Outcome       string          `json:"outcome"`
	ActorUserID   *string         `json:"actor_user_id"`
	ActorUsername *string         `json:"actor_username"`
	TargetType    *string         `json:"target_type"`
	TargetID      *string         `json:"target_id"`
	TargetName    *string         `json:"target_name"`
	TargetExists  bool            `json:"target_exists"`
	IP            *string         `json:"ip"`
	Detail        json.RawMessage `json:"detail"`
}

func toAuditItemDTO(r store.AuditRow) auditItemDTO {
	dto := auditItemDTO{
		ID: r.ID, TS: r.TS.UTC(), Event: r.Event, Outcome: r.Outcome, ActorUserID: r.ActorUserID,
		ActorUsername: r.ActorUsername, TargetType: r.TargetType, TargetID: r.TargetID, TargetName: r.TargetName, TargetExists: r.TargetExists, Detail: json.RawMessage(`{}`),
	}
	if r.IP != nil {
		s := r.IP.String()
		dto.IP = &s
	}
	if len(r.Detail) > 0 && string(r.Detail) != "null" {
		dto.Detail = r.Detail // tal como se guardo (ya saneado por la lista blanca al escribir)
	}
	return dto
}

// auditQuery es la consulta ya validada de GET /audit.
type auditQuery struct {
	filter      store.AuditFilter
	fingerprint string
	cursor      string
}

func parseAuditQuery(v url.Values) (auditQuery, error) {
	var f store.AuditFilter

	events, err := parseEventFilter(v["event"])
	if err != nil {
		return auditQuery{}, err
	}
	f.Events = events

	if len(v["outcome"]) > maxFilterValues {
		return auditQuery{}, invalidFilter("maximo %d valores en outcome", maxFilterValues)
	}
	for _, o := range v["outcome"] {
		o = strings.ToLower(strings.TrimSpace(o))
		if !validOutcomes[o] {
			return auditQuery{}, invalidFilter("outcome debe ser ok, fail o denied")
		}
		if !slices.Contains(f.Outcomes, o) {
			f.Outcomes = append(f.Outcomes, o)
		}
	}
	slices.Sort(f.Outcomes)

	// q busca en el actor, el id del objetivo y su nombre.
	q := strings.TrimSpace(v.Get("q"))
	if utf8.RuneCountInString(q) > maxActorUsernameLen || hasControl(q) {
		return auditQuery{}, invalidFilter("q excede %d caracteres o tiene caracteres invalidos", maxActorUsernameLen)
	}
	f.Query = q

	if f.From, err = parseInstant(v, "from"); err != nil {
		return auditQuery{}, err
	}
	if f.To, err = parseInstant(v, "to"); err != nil {
		return auditQuery{}, err
	}
	if f.From != nil && f.To != nil && (f.From.After(*f.To) || f.To.Sub(*f.From) > maxAuditRange) {
		return auditQuery{}, invalidFilter("from debe ser <= to y el rango no puede pasar de 366 dias")
	}

	f.Limit = store.DefaultAuditListLimit
	if raw := v.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > store.MaxAuditListLimit {
			return auditQuery{}, badRequest("limit debe ser un entero entre 1 y %d", store.MaxAuditListLimit)
		}
		f.Limit = n
	}

	cursor := v.Get("cursor")
	if len(cursor) > maxCursorLen {
		return auditQuery{}, errInvalidCursor
	}
	return auditQuery{filter: f, fingerprint: auditFingerprint(f), cursor: cursor}, nil
}

func parseEventFilter(raw []string) ([]string, error) {
	if len(raw) > maxFilterValues {
		return nil, invalidFilter("maximo %d valores en event", maxFilterValues)
	}
	var out []string
	for _, e := range raw {
		e = strings.TrimSpace(e)
		if e == "" || utf8.RuneCountInString(e) > 64 || hasControl(e) {
			return nil, invalidFilter("event invalido")
		}
		if !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	slices.Sort(out)
	return out, nil
}

// parseInstant lee un instante RFC 3339 opcional (nil si no viene).
func parseInstant(v url.Values, name string) (*time.Time, error) {
	raw := v.Get(name)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, invalidFilter("%s debe ser un instante RFC 3339", name)
	}
	t = t.UTC()
	return &t, nil
}

// auditFingerprint resume los filtros (no el limite): un cursor solo vale pa la consulta que lo emitio.
func auditFingerprint(f store.AuditFilter) string {
	b, err := json.Marshal([]any{"audit", f.Events, f.Outcomes, strings.ToLower(f.Query), f.From, f.To})
	if err != nil {
		return "" // strings y fechas: no deberia ocurrir
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

type auditCursorPayload struct {
	F  string `json:"f"`
	TS int64  `json:"t"` // microsegundos Unix (la precision de timestamptz)
	ID int64  `json:"i"`
}

func (a *API) encodeAuditCursor(fp string, c store.AuditCursor) string {
	payload, err := json.Marshal(auditCursorPayload{F: fp, TS: c.TS.UnixMicro(), ID: c.ID})
	if err != nil {
		return "" // enteros y string: no deberia ocurrir
	}
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(a.sign(payload))
}

func (a *API) decodeAuditCursor(raw, fp string) (*store.AuditCursor, error) {
	body, sig, ok := strings.Cut(raw, ".")
	if !ok {
		return nil, errInvalidCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, errInvalidCursor
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, a.sign(payload)) {
		return nil, errInvalidCursor
	}
	var p auditCursorPayload
	if err := json.Unmarshal(payload, &p); err != nil || p.F != fp || p.ID < 1 {
		return nil, errInvalidCursor
	}
	return &store.AuditCursor{TS: time.UnixMicro(p.TS).UTC(), ID: p.ID}, nil
}

// listAudit devuelve una pagina de la auditoria (solo admin, solo lectura).
func (a *API) listAudit(w http.ResponseWriter, r *http.Request) error {
	q, err := parseAuditQuery(r.URL.Query())
	if err != nil {
		return err
	}
	if q.cursor != "" {
		if q.filter.After, err = a.decodeAuditCursor(q.cursor, q.fingerprint); err != nil {
			return err
		}
	}
	page, err := a.store.ListAudit(r.Context(), q.filter)
	if err != nil {
		return err
	}
	items := make([]auditItemDTO, 0, len(page.Items))
	for _, row := range page.Items {
		items = append(items, toAuditItemDTO(row))
	}
	var next *string
	if page.NextCursor != nil {
		c := a.encodeAuditCursor(q.fingerprint, *page.NextCursor)
		next = &c
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next, "total": page.Total})
	return nil
}

// listAuditEvents devuelve los nombres de evento existentes pa poblar el filtro.
func (a *API) listAuditEvents(w http.ResponseWriter, r *http.Request) error {
	events, err := a.store.AuditEvents(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
	return nil
}
