package httpapi

import (
	"errors"
	"math"
	"net/http"
	"time"

	"argos/server/internal/store"
)

// maxConcurrentReports es cuantos informes de disponibilidad pueden calcularse a la vez (son consultas pesadas).
const maxConcurrentReports = 2

// reportRanges son los rangos admitidos (ventana movil que termina ahora).
var reportRanges = map[string]time.Duration{
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
	"90d": 90 * 24 * time.Hour,
}

var errReportTooLarge = &clientError{status: http.StatusUnprocessableEntity, code: "narrow_with_group_id",
	msg: "el informe abarca demasiados hosts: acota con group_id"}

var errReportTimeout = &clientError{status: http.StatusServiceUnavailable, code: "report_timeout",
	msg: "el informe tardo demasiado: acota con group_id o un rango menor", retryAfter: 30 * time.Second}

// reportParams son los parametros comunes de los informes ya validados.
type reportParams struct {
	rangeName string
	from, to  time.Time
	groupID   *string
}

func (a *API) parseReportParams(r *http.Request) (reportParams, error) {
	v := r.URL.Query()
	p := reportParams{rangeName: v.Get("range")}
	if p.rangeName == "" {
		p.rangeName = "30d"
	}
	span, ok := reportRanges[p.rangeName]
	if !ok {
		return p, badRequest("range debe ser 7d, 30d o 90d")
	}
	p.to = a.now().UTC().Truncate(time.Second)
	p.from = p.to.Add(-span)
	if raw := v.Get("group_id"); raw != "" {
		if !isUUID(raw) {
			return p, store.ErrNotFound // mismo 404 que un grupo ajeno o inexistente
		}
		p.groupID = &raw
	}
	return p, nil
}

// acquireReport reserva uno de los cupos de informes concurrentes; devuelve como liberarlo.
func (a *API) acquireReport() (release func(), err error) {
	select {
	case a.reportSem <- struct{}{}:
		return func() { <-a.reportSem }, nil
	default:
		return nil, rateLimited(5 * time.Second)
	}
}

// reportErr traduce los errores propios de los informes; el resto pasa tal cual (writeErr los maneja).
func (a *API) reportErr(r *http.Request, err error) error {
	switch {
	case errors.Is(err, store.ErrReportTooLarge):
		return errReportTooLarge
	case errors.Is(err, store.ErrReportTimeout):
		a.logger.WarnContext(r.Context(), "informe supero el statement_timeout", "path", r.URL.Path)
		return errReportTimeout
	}
	return err
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

func pct3(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := round3(*p)
	return &v
}

type availabilityTotalsDTO struct {
	Hosts           int      `json:"hosts"`
	UpS             int64    `json:"up_s"`
	DownS           int64    `json:"down_s"`
	DownMaintS      int64    `json:"down_maint_s"`
	OfflineS        int64    `json:"offline_s"`
	Incidents       int      `json:"incidents"`
	AvailabilityPct *float64 `json:"availability_pct"`
	CoveragePct     float64  `json:"coverage_pct"`
}

func toTotalsDTO(t store.AvailabilityTotals, from, to time.Time) availabilityTotalsDTO {
	return availabilityTotalsDTO{Hosts: t.Hosts, UpS: t.UpS, DownS: t.DownS, DownMaintS: t.DownMaintS, OfflineS: t.OfflineS,
		Incidents: t.Incidents, AvailabilityPct: pct3(t.Pct()), CoveragePct: round3(t.CoveragePct(from, to))}
}

type hostAvailabilityDTO struct {
	HostID    string  `json:"host_id"`
	Hostname  string  `json:"hostname"`
	GroupID   *string `json:"group_id"`
	GroupName *string `json:"group_name"`
	availabilityTotalsDTO
	TrackedFrom *time.Time `json:"tracked_from"`
}

type groupAvailabilityDTO struct {
	GroupID   *string `json:"group_id"`
	GroupName *string `json:"group_name"`
	availabilityTotalsDTO
}

// availabilityReport: GET /reports/availability?range=7d|30d|90d&by=host|group&group_id=
func (a *API) availabilityReport(w http.ResponseWriter, r *http.Request) error {
	p, err := a.parseReportParams(r)
	if err != nil {
		return err
	}
	by := r.URL.Query().Get("by")
	if by == "" {
		by = "host"
	}
	if by != "host" && by != "group" {
		return badRequest("by debe ser host o group")
	}
	release, err := a.acquireReport()
	if err != nil {
		return err
	}
	defer release()

	hosts, err := a.store.AvailabilityReport(r.Context(), currentUser(r).Scope, p.groupID, p.from, p.to)
	if err != nil {
		return a.reportErr(r, err)
	}
	var items any
	if by == "group" {
		out := make([]groupAvailabilityDTO, 0)
		for _, g := range store.GroupTotals(hosts, p.from, p.to) {
			out = append(out, groupAvailabilityDTO{GroupID: g.GroupID, GroupName: g.GroupName,
				availabilityTotalsDTO: toTotalsDTO(g.AvailabilityTotals, p.from, p.to)})
		}
		items = out
	} else {
		out := make([]hostAvailabilityDTO, 0, len(hosts))
		for _, h := range hosts {
			t := store.Totals([]store.HostAvailability{h}, p.from, p.to)
			out = append(out, hostAvailabilityDTO{HostID: h.HostID, Hostname: h.Hostname, GroupID: h.GroupID, GroupName: h.GroupName,
				availabilityTotalsDTO: toTotalsDTO(t, p.from, p.to), TrackedFrom: utcPtr(h.TrackedFrom)})
		}
		items = out
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"range": p.rangeName, "by": by, "from": p.from, "to": p.to, "group_id": p.groupID,
		"fleet": toTotalsDTO(store.Totals(hosts, p.from, p.to), p.from, p.to), "items": items,
	})
	return nil
}

type checkAvailabilityDTO struct {
	HostID          string     `json:"host_id"`
	Hostname        string     `json:"hostname"`
	GroupID         *string    `json:"group_id"`
	GroupName       *string    `json:"group_name"`
	CheckID         string     `json:"check_id"`
	CheckName       string     `json:"check_name"`
	UpS             int64      `json:"up_s"`
	WarnS           int64      `json:"warn_s"`
	DownS           int64      `json:"down_s"`
	DownMaintS      int64      `json:"down_maint_s"`
	Incidents       int        `json:"incidents"`
	AvailabilityPct *float64   `json:"availability_pct"`
	TrackedFrom     *time.Time `json:"tracked_from"`
	CoveragePct     float64    `json:"coverage_pct"`
}

type checksFleetDTO struct {
	Items           int      `json:"items"`
	UpS             int64    `json:"up_s"`
	WarnS           int64    `json:"warn_s"`
	DownS           int64    `json:"down_s"`
	DownMaintS      int64    `json:"down_maint_s"`
	Incidents       int      `json:"incidents"`
	AvailabilityPct *float64 `json:"availability_pct"`
}

// serviceChecksReport: GET /reports/service-checks?range=7d|30d|90d&group_id= (una fila por host y chequeo).
func (a *API) serviceChecksReport(w http.ResponseWriter, r *http.Request) error {
	p, err := a.parseReportParams(r)
	if err != nil {
		return err
	}
	release, err := a.acquireReport()
	if err != nil {
		return err
	}
	defer release()

	rows, err := a.store.ServiceChecksReport(r.Context(), currentUser(r).Scope, p.groupID, p.from, p.to)
	if err != nil {
		return a.reportErr(r, err)
	}
	items := make([]checkAvailabilityDTO, 0, len(rows))
	var sum store.CheckAvailability
	for _, c := range rows {
		items = append(items, checkAvailabilityDTO{HostID: c.HostID, Hostname: c.Hostname, GroupID: c.GroupID, GroupName: c.GroupName,
			CheckID: c.CheckID, CheckName: c.CheckName, UpS: c.UpS, WarnS: c.WarnS, DownS: c.DownS, DownMaintS: c.DownMaintS,
			Incidents: c.Incidents, AvailabilityPct: pct3(c.Pct()), TrackedFrom: utcPtr(c.TrackedFrom),
			CoveragePct: round3(c.CoveragePct(p.from, p.to))})
		sum.UpS += c.UpS
		sum.WarnS += c.WarnS
		sum.DownS += c.DownS
		sum.DownMaintS += c.DownMaintS
		sum.Incidents += c.Incidents
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"range": p.rangeName, "from": p.from, "to": p.to, "group_id": p.groupID,
		"fleet": checksFleetDTO{Items: len(rows), UpS: sum.UpS, WarnS: sum.WarnS, DownS: sum.DownS, DownMaintS: sum.DownMaintS,
			Incidents: sum.Incidents, AvailabilityPct: pct3(sum.Pct())},
		"items": items,
	})
	return nil
}
