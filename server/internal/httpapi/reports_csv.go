package httpapi

import (
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"argos/server/internal/store"
)

const (
	// csvFlushEvery es cada cuantas filas se vacia el buffer hacia el cliente.
	csvFlushEvery = 500
	// csvWriteWindow amplia el WriteTimeout global (10 s) pa una exportacion grande; el tope real es el de filas.
	csvWriteWindow = 5 * time.Minute
)

var errExportTooLarge = &clientError{status: http.StatusUnprocessableEntity, code: "export_too_large",
	msg: "la exportacion supera el maximo de filas: acota con group_id o un rango menor"}

// csvSafe neutraliza la inyeccion de formulas en Excel/Sheets: una celda de texto cuyo primer caracter (tras los
// espacios iniciales) sea = + - @ tab o CR se prefija con una comilla simple. Se aplica a TODO texto (el hostname
// lo controla el agente); los numeros se escriben desde su tipo numerico, nunca por aqui.
func csvSafe(s string) string {
	t := strings.TrimLeft(s, " ")
	if t != "" && strings.ContainsRune("=+-@\t\r", rune(t[0])) {
		return "'" + s
	}
	return s
}

// csvExport escribe un CSV en streaming (BOM UTF-8 pa Excel, cabecera fija) y cuenta las filas escritas.
type csvExport struct {
	w        http.ResponseWriter
	rc       *http.ResponseController
	cw       *csv.Writer
	filename string
	header   []string
	rows     int
	started  bool
}

func newCSVExport(w http.ResponseWriter, filename string, header []string) *csvExport {
	return &csvExport{w: w, rc: http.NewResponseController(w), filename: filename, header: header}
}

// start envia las cabeceras HTTP, el BOM y la fila de titulos. Antes de start aun se puede responder un error JSON.
func (e *csvExport) start() error {
	if e.started {
		return nil
	}
	e.started = true
	// Sin soporte de deadline (p. ej. httptest) no pasa nada: rige el del servidor.
	if err := e.rc.SetWriteDeadline(time.Now().Add(csvWriteWindow)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	h := e.w.Header()
	h.Set("Content-Type", "text/csv; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="`+e.filename+`"`)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	e.w.WriteHeader(http.StatusOK)
	if _, err := e.w.Write([]byte("\xEF\xBB\xBF")); err != nil {
		return err
	}
	e.cw = csv.NewWriter(e.w)
	return e.cw.Write(e.header)
}

// row escribe una fila (las celdas de texto ya deben venir por csvSafe).
func (e *csvExport) row(cells ...string) error {
	if err := e.start(); err != nil {
		return err
	}
	if err := e.cw.Write(cells); err != nil {
		return err
	}
	e.rows++
	if e.rows%csvFlushEvery == 0 {
		return e.flush()
	}
	return nil
}

func (e *csvExport) flush() error {
	e.cw.Flush()
	if err := e.cw.Error(); err != nil {
		return err
	}
	if err := e.rc.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

// finish asegura que aunque no haya filas salga el CSV con su cabecera, y vacia el buffer.
func (e *csvExport) finish() error {
	if err := e.start(); err != nil {
		return err
	}
	return e.flush()
}

// csvFilename arma "argos-<kind>[-<rangeName>]-AAAAMMDD.csv"; rangeName vacio (exports de inventario, sin rango
// de fechas) no deja un guion colgando.
func csvFilename(kind, rangeName string, now time.Time) string {
	if rangeName == "" {
		return "argos-" + kind + "-" + now.UTC().Format("20060102") + ".csv"
	}
	return "argos-" + kind + "-" + rangeName + "-" + now.UTC().Format("20060102") + ".csv"
}

func csvPct(p *float64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatFloat(round3(*p), 'f', 3, 64)
}

func csvTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func csvStrPtr(s *string) string {
	if s == nil {
		return ""
	}
	return csvSafe(*s)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// auditExport registra la exportacion (tipo, rango, grupo y filas; nunca contenido). Al terminar y tambien si el
// cliente aborta, con las filas realmente escritas.
func (a *API) auditExport(r *http.Request, kind string, p reportParams, rows int, outcome, reason string) {
	d := map[string]any{"report": kind, "range": p.rangeName, "rows": rows}
	if p.groupID != nil {
		d["group_id"] = *p.groupID
	}
	if reason != "" {
		d["reason"] = reason
	}
	e := actor(currentUser(r))
	e.Event, e.Outcome, e.TargetType = evReportExported, outcome, "report"
	a.audit(r, e, d)
}

// finishExport cierra el CSV y audita. Un error a mitad del stream ya no puede cambiar el status: se loguea.
func (a *API) finishExport(r *http.Request, kind string, p reportParams, ex *csvExport, err error) error {
	if err == nil {
		err = ex.finish()
	}
	switch {
	case err == nil:
		a.auditExport(r, kind, p, ex.rows, store.OutcomeOK, "")
		return nil
	case errors.Is(err, store.ErrExportTooLarge):
		a.auditExport(r, kind, p, 0, store.OutcomeFail, "too_large")
		return errExportTooLarge
	case errors.Is(err, store.ErrReportTooLarge):
		a.auditExport(r, kind, p, 0, store.OutcomeFail, "too_large")
		return errReportTooLarge
	case ex.started:
		a.logger.WarnContext(r.Context(), "exportacion csv interrumpida", "report", kind, "rows", ex.rows, "err", err)
		a.auditExport(r, kind, p, ex.rows, store.OutcomeFail, "aborted")
		return nil
	default:
		return a.reportErr(r, err) // ErrNotFound, timeout, etc.: nada se escribio aun
	}
}

// availabilityHeader son las columnas de disponibilidad.csv, compartidas entre el informe en streaming y el export
// del cliente (zip, B17).
var availabilityHeader = []string{"host_id", "hostname", "grupo", "desde", "hasta", "up_s", "down_s",
	"down_mantenimiento_s", "offline_s", "incidentes", "disponibilidad_pct", "cobertura_pct", "historial_desde"}

// availabilityRow arma las celdas de una fila de disponibilidad.csv para un host.
func availabilityRow(h store.HostAvailability, from, to time.Time) []string {
	t := store.Totals([]store.HostAvailability{h}, from, to)
	cov := round3(t.CoveragePct(from, to))
	return []string{csvSafe(h.HostID), csvSafe(h.Hostname), csvStrPtr(h.GroupName), from.UTC().Format(time.RFC3339),
		to.UTC().Format(time.RFC3339), itoa(t.UpS), itoa(t.DownS), itoa(t.DownMaintS), itoa(t.OfflineS),
		strconv.Itoa(t.Incidents), csvPct(t.Pct()), csvPct(&cov), csvTime(h.TrackedFrom)}
}

// writeAvailabilityCSV escribe disponibilidad.csv (cabecera y una fila por host) en cualquier io.Writer; lo usa el
// export del cliente (zip) para reusar exactamente las mismas columnas que el informe en streaming.
func writeAvailabilityCSV(w io.Writer, hosts []store.HostAvailability, from, to time.Time) (int, error) {
	cw := csv.NewWriter(w)
	if err := cw.Write(availabilityHeader); err != nil {
		return 0, err
	}
	rows := 0
	for _, h := range hosts {
		if err := cw.Write(availabilityRow(h, from, to)); err != nil {
			return rows, err
		}
		rows++
	}
	cw.Flush()
	return rows, cw.Error()
}

// availabilityCSV: GET /reports/availability.csv?range=7d|30d|90d&group_id= (una fila por host).
func (a *API) availabilityCSV(w http.ResponseWriter, r *http.Request) error {
	p, err := a.parseReportParams(r)
	if err != nil {
		return err
	}
	release, err := a.acquireReport()
	if err != nil {
		return err
	}
	defer release()

	ex := newCSVExport(w, csvFilename("disponibilidad", p.rangeName, p.to), availabilityHeader)
	// El informe ya acota a MaxReportHosts (2000) filas: cabe en memoria y falla con 422 antes de escribir.
	hosts, err := a.store.AvailabilityReport(r.Context(), currentUser(r).Scope, p.groupID, p.from, p.to)
	if err == nil {
		for _, h := range hosts {
			if err = ex.row(availabilityRow(h, p.from, p.to)...); err != nil {
				break
			}
		}
	}
	return a.finishExport(r, "availability", p, ex, err)
}

// serviceChecksCSV: GET /reports/service-checks.csv?range=7d|30d|90d&group_id= (una fila por host y chequeo).
func (a *API) serviceChecksCSV(w http.ResponseWriter, r *http.Request) error {
	p, err := a.parseReportParams(r)
	if err != nil {
		return err
	}
	release, err := a.acquireReport()
	if err != nil {
		return err
	}
	defer release()

	ex := newCSVExport(w, csvFilename("chequeos", p.rangeName, p.to), []string{"host_id", "hostname", "grupo", "chequeo_id",
		"chequeo", "desde", "hasta", "up_s", "warn_s", "down_s", "down_mantenimiento_s", "incidentes", "disponibilidad_pct",
		"cobertura_pct", "historial_desde"})
	// El informe ya acota a MaxReportRows (5000) filas: cabe en memoria y falla con 422 antes de escribir.
	rows, err := a.store.ServiceChecksReport(r.Context(), currentUser(r).Scope, p.groupID, p.from, p.to)
	if err == nil {
		from, to := p.from.UTC().Format(time.RFC3339), p.to.UTC().Format(time.RFC3339)
		for _, c := range rows {
			cov := round3(c.CoveragePct(p.from, p.to))
			if err = ex.row(csvSafe(c.HostID), csvSafe(c.Hostname), csvStrPtr(c.GroupName), csvSafe(c.CheckID), csvSafe(c.CheckName),
				from, to, itoa(c.UpS), itoa(c.WarnS), itoa(c.DownS), itoa(c.DownMaintS), strconv.Itoa(c.Incidents),
				csvPct(c.Pct()), csvPct(&cov), csvTime(c.TrackedFrom)); err != nil {
				break
			}
		}
	}
	return a.finishExport(r, "service-checks", p, ex, err)
}

// alertsCSV: GET /reports/alerts.csv?range=7d|30d|90d&state=open,acknowledged,resolved&group_id= (una fila por
// alerta abierta en el periodo). Streaming real: no carga las filas en memoria.
func (a *API) alertsCSV(w http.ResponseWriter, r *http.Request) error {
	p, err := a.parseReportParams(r)
	if err != nil {
		return err
	}
	states, err := parseEnumList(r.URL.Query(), "state", validAlertStates)
	if err != nil {
		return err
	}
	release, err := a.acquireReport()
	if err != nil {
		return err
	}
	defer release()

	ex := newCSVExport(w, csvFilename("alertas", p.rangeName, p.to), []string{"alerta_id", "abierta", "resuelta", "duracion_s",
		"host_id", "hostname", "grupo", "regla", "severidad", "estado", "suprimida", "reconocida_por", "reconocida"})
	_, err = a.store.ExportAlerts(r.Context(), currentUser(r).Scope,
		store.AlertExportFilter{From: p.from, To: p.to, GroupID: p.groupID, States: states},
		func(al store.AlertExportRow) error {
			if err := r.Context().Err(); err != nil {
				return err // el cliente se fue
			}
			dur := ""
			if al.ResolvedAt != nil {
				dur = itoa(int64(al.ResolvedAt.Sub(al.OpenedAt).Seconds()))
			}
			return ex.row(csvSafe(al.ID), csvTime(&al.OpenedAt), csvTime(al.ResolvedAt), dur, csvSafe(al.HostID), csvSafe(al.Hostname),
				csvStrPtr(al.GroupName), csvSafe(al.RuleName), csvSafe(al.Severity), csvSafe(al.State),
				strconv.FormatBool(al.Suppressed), csvStrPtr(al.AcknowledgedByName), csvTime(al.AcknowledgedAt))
		})
	return a.finishExport(r, "alerts", p, ex, err)
}
