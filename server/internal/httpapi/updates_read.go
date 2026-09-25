package httpapi

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"argos/server/internal/store"
)

const (
	// updatesStaleCacheAge: con la cache de paquetes mas vieja que esto el dato puede estar desactualizado.
	updatesStaleCacheAge = 7 * 24 * 3600 // segundos
	// maxUpdatesHistory acota ?history= del detalle del host; defaultUpdatesHistory es su valor por defecto.
	maxUpdatesHistory     = 50
	defaultUpdatesHistory = 20
	maxEOLDaysFilter      = 3650
	updatesAuditModule    = "updates"
	updatesAuditHost      = "host"
	updatesAuditFleet     = "fleet"
	dateLayout            = "2006-01-02"
)

// updatesKnownDTO dice que campos de la foto se conocen (false = desconocido: el agente no pudo determinarlo).
type updatesKnownDTO struct {
	PendingTotal    bool `json:"pending_total"`
	PendingSecurity bool `json:"pending_security"`
	RebootRequired  bool `json:"reboot_required"`
	LastInstalled   bool `json:"last_installed"`
}

// hostUpdatesDTO es la foto de parches de un host. Un campo null = desconocido (nunca 0 ni false).
type hostUpdatesDTO struct {
	PendingTotal         *int            `json:"pending_total"`
	PendingSecurity      *int            `json:"pending_security"`
	RebootRequired       *bool           `json:"reboot_required"`
	RebootSince          *time.Time      `json:"reboot_since"`
	SecurityPendingSince *time.Time      `json:"security_pending_since"`
	LastInstalled        *string         `json:"last_installed"` // YYYY-MM-DD
	Known                updatesKnownDTO `json:"known"`
	Source               string          `json:"source"`
	CacheAgeS            *int64          `json:"cache_age_s"`
	Stale                bool            `json:"stale"` // cache de paquetes de mas de 7 dias: posiblemente desactualizado
	DataAgeS             int64           `json:"data_age_s"`
	CollectedAt          time.Time       `json:"collected_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
	Advisories           []string        `json:"advisories"`
	AdvisoriesTruncated  bool            `json:"advisories_truncated"`
}

// eolDTO es el fin de vida resuelto del SO de un host.
type eolDTO struct {
	Label     string `json:"label"`
	EOLDate   string `json:"eol_date"` // YYYY-MM-DD
	DaysLeft  int    `json:"days_left"`
	Expired   bool   `json:"expired"`
	Estimated bool   `json:"estimated"`
	LTS       bool   `json:"lts"`
}

type updatesPointDTO struct {
	At              time.Time `json:"at"`
	PendingTotal    *int      `json:"pending_total"`
	PendingSecurity *int      `json:"pending_security"`
	RebootRequired  *bool     `json:"reboot_required"`
}

type hostUpdatesResponse struct {
	Status  string            `json:"status"` // ok | no_data (el host aun no reporto el modulo o esta desactivado en el agente)
	Updates *hostUpdatesDTO   `json:"updates"`
	EOL     *eolDTO           `json:"eol"` // null = sin dato de fin de vida para su SO
	History []updatesPointDTO `json:"history"`
}

func dateStr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(dateLayout)
	return &s
}

func toHostUpdatesDTO(u store.UpdatesCurrent, now time.Time) hostUpdatesDTO {
	adv := u.Advisories
	if adv == nil {
		adv = []string{}
	}
	return hostUpdatesDTO{
		PendingTotal: u.PendingTotal, PendingSecurity: u.PendingSecurity, RebootRequired: u.RebootRequired,
		RebootSince: utcPtr(u.RebootSince), SecurityPendingSince: utcPtr(u.SecurityPendingSince), LastInstalled: dateStr(u.LastInstalled),
		Known: updatesKnownDTO{PendingTotal: u.PendingTotal != nil, PendingSecurity: u.PendingSecurity != nil,
			RebootRequired: u.RebootRequired != nil, LastInstalled: u.LastInstalled != nil},
		Source: u.Source, CacheAgeS: u.CacheAgeS, Stale: u.CacheAgeS != nil && *u.CacheAgeS > updatesStaleCacheAge,
		DataAgeS:    max(int64(now.Sub(u.CollectedAt).Seconds()), 0),
		CollectedAt: u.CollectedAt.UTC(), UpdatedAt: u.ReceivedAt.UTC(),
		Advisories: adv, AdvisoriesTruncated: u.AdvisoriesTruncated,
	}
}

func toEOLDTO(e *store.OSEOL, now time.Time) *eolDTO {
	if e == nil {
		return nil
	}
	today := now.UTC().Truncate(24 * time.Hour)
	days := int(e.EOLDate.UTC().Truncate(24*time.Hour).Sub(today).Hours() / 24)
	return &eolDTO{Label: e.Label, EOLDate: e.EOLDate.UTC().Format(dateLayout), DaysLeft: days, Expired: days < 0,
		Estimated: e.Estimated, LTS: e.LTS}
}

// auditUpdatesRead registra la lectura (actor y destino: host o flota; nunca contenido), una vez por
// (usuario, destino) cada invAuditWindow pa que un refresco del frontend no inunde audit_log.
func (a *API) auditUpdatesRead(r *http.Request, u *sessionUser, hostID string) {
	target, scope := "fleet", updatesAuditFleet
	if hostID != "" {
		target, scope = hostID, updatesAuditHost
	}
	if ok, _ := a.updatesReadDedup.allow(u.ID+"|"+target, a.now()); !ok {
		return
	}
	e := actor(u)
	e.Event, e.Outcome = evUpdatesRead, store.OutcomeOK
	if hostID != "" {
		e.TargetType, e.TargetID = "host", hostID
	}
	a.audit(r, e, map[string]any{"module": updatesAuditModule, "scope": scope})
}

// hostUpdates devuelve la foto de parches del host, su fin de vida resuelto y el historial reciente
// (?history=N, 0-50, por defecto 20).
func (a *API) hostUpdates(w http.ResponseWriter, r *http.Request) error {
	u, id := currentUser(r), strings.ToLower(r.PathValue("id"))
	history := defaultUpdatesHistory
	if raw := r.URL.Query().Get("history"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > maxUpdatesHistory {
			return badRequest("history debe ser un entero entre 0 y %d", maxUpdatesHistory)
		}
		history = n
	}
	cur, found, err := a.store.HostUpdates(r.Context(), u.Scope, id)
	if err != nil {
		return err
	}
	resp := hostUpdatesResponse{Status: "no_data", History: []updatesPointDTO{}}
	if found {
		now := a.now()
		dto := toHostUpdatesDTO(cur, now)
		resp.Status, resp.Updates = "ok", &dto
		eol, ok, err := a.store.HostOSEOL(r.Context(), u.Scope, id)
		if err != nil {
			return err
		}
		if ok {
			resp.EOL = toEOLDTO(&eol, now)
		}
		if history > 0 {
			points, err := a.store.HostUpdatesHistory(r.Context(), u.Scope, id, history)
			if err != nil {
				return err
			}
			for _, p := range points {
				resp.History = append(resp.History, updatesPointDTO{At: p.At.UTC(), PendingTotal: p.PendingTotal,
					PendingSecurity: p.PendingSecurity, RebootRequired: p.RebootRequired})
			}
		}
	}
	a.auditUpdatesRead(r, u, id)
	writeJSON(w, http.StatusOK, resp)
	return nil
}

// --- flota ---

type fleetItemDTO struct {
	Host            hostRefDTO   `json:"host"`
	Status          string       `json:"status"`
	Group           *groupRefDTO `json:"group"`
	OS              string       `json:"os"`
	OSName          *string      `json:"os_name"`
	OSVersion       *string      `json:"os_version"`
	PendingTotal    *int         `json:"pending_total"`
	PendingSecurity *int         `json:"pending_security"`
	RebootRequired  *bool        `json:"reboot_required"`
	RebootSince     *time.Time   `json:"reboot_since"`
	LastInstalled   *string      `json:"last_installed"`
	Source          string       `json:"source"`
	CacheAgeS       *int64       `json:"cache_age_s"`
	Stale           bool         `json:"stale"`
	DataAgeS        int64        `json:"data_age_s"`
	UpdatedAt       time.Time    `json:"updated_at"`
	EOL             *eolDTO      `json:"eol"`
}

const defaultFleetSort = "security"

type fleetQuery struct {
	filter      store.UpdatesFleetFilter
	fingerprint string
	cursor      string
}

// parseBoolFlag lee un filtro booleano opcional (true/1 o false/0; vacio = false).
func parseBoolFlag(v url.Values, name string) (bool, error) {
	switch raw := v.Get(name); raw {
	case "", "false", "0":
		return false, nil
	case "true", "1":
		return true, nil
	default:
		return false, invalidFilter("%s debe ser true o false", name)
	}
}

func parseFleetQuery(v url.Values, scope store.Scope) (fleetQuery, error) {
	var f store.UpdatesFleetFilter
	q := strings.TrimSpace(v.Get("q"))
	if utf8.RuneCountInString(q) > maxQueryLen || hasControl(q) {
		return fleetQuery{}, badRequest("q excede %d caracteres o tiene caracteres invalidos", maxQueryLen)
	}
	f.Q = q

	var hf store.HostFilter // solo pa reutilizar el parseo de group
	if err := parseGroupFilter(v["group"], &hf); err != nil {
		return fleetQuery{}, err
	}
	f.GroupIDs, f.NoGroup = hf.GroupIDs, hf.NoGroup
	var err error
	if f.OSes, err = parseOSFilter(v["os"]); err != nil {
		return fleetQuery{}, err
	}
	if f.SecurityOnly, err = parseBoolFlag(v, "security"); err != nil {
		return fleetQuery{}, err
	}
	if f.RebootOnly, err = parseBoolFlag(v, "reboot"); err != nil {
		return fleetQuery{}, err
	}
	if raw := v.Get("eol_days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > maxEOLDaysFilter {
			return fleetQuery{}, invalidFilter("eol_days debe ser un entero entre 0 y %d", maxEOLDaysFilter)
		}
		f.EOLWithinDays = &n
	}

	f.Sort = v.Get("sort")
	if f.Sort == "" {
		f.Sort = defaultFleetSort
	}
	defaultDesc, ok := store.UpdatesFleetSortDefaultDesc(f.Sort)
	if !ok {
		return fleetQuery{}, &clientError{status: http.StatusBadRequest, code: "invalid_sort",
			msg: "sort debe ser security, pending, reboot, eol, name o updated"}
	}
	switch d := v.Get("dir"); d {
	case "":
		f.Desc = defaultDesc
	case "asc":
	case "desc":
		f.Desc = true
	default:
		return fleetQuery{}, badRequest("dir debe ser asc o desc")
	}

	f.Limit = store.DefaultUpdatesFleetLimit
	if raw := v.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > store.MaxUpdatesFleetLimit {
			return fleetQuery{}, badRequest("limit debe ser un entero entre 1 y %d", store.MaxUpdatesFleetLimit)
		}
		f.Limit = n
	}
	cursor := v.Get("cursor")
	if len(cursor) > maxCursorLen {
		return fleetQuery{}, errInvalidCursor
	}
	eol := -1
	if f.EOLWithinDays != nil {
		eol = *f.EOLWithinDays
	}
	fp := fingerprintOf("updates_fleet", f.Q, f.GroupIDs, f.NoGroup, f.OSes, f.SecurityOnly, f.RebootOnly, eol,
		f.Sort, f.Desc, scope.Key())
	return fleetQuery{filter: f, fingerprint: fp, cursor: cursor}, nil
}

// updatesFleet lista, por cursor y dentro del ambito del usuario, los hosts con foto de parches.
func (a *API) updatesFleet(w http.ResponseWriter, r *http.Request) error {
	u := currentUser(r)
	q, err := parseFleetQuery(r.URL.Query(), u.Scope)
	if err != nil {
		return err
	}
	if q.cursor != "" {
		if q.filter.After, err = a.decodeCursor(q.cursor, q.fingerprint); err != nil {
			return err
		}
	}
	page, err := a.store.ListUpdatesFleet(r.Context(), u.Scope, q.filter)
	if err != nil {
		return err
	}
	now := a.now()
	items := make([]fleetItemDTO, 0, len(page.Items))
	for _, x := range page.Items {
		d := toHostUpdatesDTO(x.Updates, now)
		items = append(items, fleetItemDTO{
			Host: hostRefDTO{ID: x.HostID, Hostname: x.Hostname}, Status: x.Status, Group: toGroupRefDTO(x.Group),
			OS: x.OS, OSName: x.OSName, OSVersion: x.OSVersion,
			PendingTotal: d.PendingTotal, PendingSecurity: d.PendingSecurity, RebootRequired: d.RebootRequired,
			RebootSince: d.RebootSince, LastInstalled: d.LastInstalled, Source: d.Source, CacheAgeS: d.CacheAgeS,
			Stale: d.Stale, DataAgeS: d.DataAgeS, UpdatedAt: d.UpdatedAt, EOL: toEOLDTO(x.EOL, now),
		})
	}
	var next *string
	if page.NextCursor != nil {
		c := a.encodeCursor(q.fingerprint, *page.NextCursor)
		next = &c
	}
	a.auditUpdatesRead(r, u, "")
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next, "total": page.Total})
	return nil
}
