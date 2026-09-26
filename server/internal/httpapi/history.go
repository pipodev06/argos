package httpapi

import (
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"argos/server/internal/store"
)

const (
	// Topes de rango por consulta segun la resolucion.
	historyRawMaxDays    = 31
	historyHourlyMaxDays = 366
	// historyFutureSlack es cuanto se tolera "to" en el futuro (desfase de relojes).
	historyFutureSlack = 5 * time.Minute
	// historyRawGapSeconds: en el CSV crudo una tasa de red no se calcula si el tramo pasa de 10 min.
	historyRawGapSeconds = 600

	// Exportaciones: por usuario y minuto, y a la vez en todo el servidor (cupo propio, distinto del de informes).
	exportsPerMinute     = 6
	maxConcurrentExports = 3

	evHistoryExport = "history_export"
)

// historyMinFrom rechaza fechas absurdas (no hay datos ni de lejos anteriores).
var historyMinFrom = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// exportGate reparte el cupo de exportaciones CSV: una a la vez por usuario y un tope global pequeño, de modo que
// un cliente no acapare el cupo de los demas.
type exportGate struct {
	mu     sync.Mutex
	active map[string]struct{}
	slots  chan struct{}
}

func newExportGate(global int) *exportGate {
	return &exportGate{active: map[string]struct{}{}, slots: make(chan struct{}, global)}
}

// acquire reserva el cupo del usuario; devuelve como liberarlo, o un 429 con Retry-After.
func (g *exportGate) acquire(userID string) (release func(), err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, busy := g.active[userID]; busy {
		return nil, rateLimited(10 * time.Second)
	}
	select {
	case g.slots <- struct{}{}:
	default:
		return nil, rateLimited(10 * time.Second)
	}
	g.active[userID] = struct{}{}
	var once sync.Once
	return func() {
		once.Do(func() { // idempotente: liberar dos veces no puede robar el cupo de otro
			g.mu.Lock()
			defer g.mu.Unlock()
			delete(g.active, userID)
			<-g.slots
		})
	}, nil
}

// historyParams son los parametros validados de GET /hosts/{id}/history.
type historyParams struct {
	from, to   time.Time
	csv        bool
	disks      bool
	resolution string // raw | hourly
}

func invalidRange(msg string) error {
	return &clientError{status: http.StatusBadRequest, code: "invalid_range", msg: msg}
}

func parseHistoryInstant(name, raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, invalidRange(name + " es obligatorio (RFC3339)")
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, invalidRange(name + " debe ser una fecha RFC3339")
	}
	return t.UTC(), nil
}

// parseHistory valida los parametros y decide la resolucion segun la retencion de detalle efectiva del host.
func parseHistory(r *http.Request, now time.Time, detailDays int) (historyParams, error) {
	q := r.URL.Query()
	var p historyParams
	switch q.Get("format") {
	case "", "json":
	case "csv":
		p.csv = true
	default:
		return p, badRequest("format debe ser json o csv")
	}
	switch q.Get("series") {
	case "", "metrics":
	case "disks":
		p.disks = true
		if !p.csv {
			return p, badRequest("series=disks solo se exporta con format=csv")
		}
	default:
		return p, badRequest("series debe ser metrics o disks")
	}

	var err error
	if p.from, err = parseHistoryInstant("from", q.Get("from")); err != nil {
		return p, err
	}
	if p.to, err = parseHistoryInstant("to", q.Get("to")); err != nil {
		return p, err
	}
	switch {
	case p.from.Before(historyMinFrom):
		return p, invalidRange("from es demasiado antiguo")
	case !p.to.After(p.from):
		return p, invalidRange("to debe ser posterior a from")
	case p.to.After(now.Add(historyFutureSlack)):
		return p, invalidRange("to no puede estar en el futuro")
	}

	p.resolution = "hourly"
	if !p.from.Before(now.Add(-time.Duration(detailDays) * 24 * time.Hour)) {
		p.resolution = "raw"
	}
	maxDays := historyHourlyMaxDays
	if p.resolution == "raw" || p.disks { // disk_samples solo existe con detalle: mismo tope que el crudo
		maxDays = historyRawMaxDays
	}
	if p.to.Sub(p.from) > time.Duration(maxDays)*24*time.Hour {
		return p, &clientError{status: http.StatusBadRequest, code: "range_too_large",
			msg:     "el rango supera el maximo de " + strconv.Itoa(maxDays) + " dias para esta resolucion",
			details: map[string]any{"max_days": maxDays, "resolution": p.resolution}}
	}
	return p, nil
}

// rawStep elige el paso del detalle de 30 s pa no pasar de ~600 puntos.
func rawStep(span time.Duration) time.Duration {
	for _, s := range []time.Duration{30 * time.Second, time.Minute, 5 * time.Minute, 10 * time.Minute, 30 * time.Minute} {
		if span/s <= 600 {
			return s
		}
	}
	return time.Hour
}

// hourlyStep elige el paso de los resumenes por hora (1 h, 3 h o 12 h).
func hourlyStep(span time.Duration) time.Duration {
	for _, s := range []time.Duration{time.Hour, 3 * time.Hour} {
		if span/s <= 600 {
			return s
		}
	}
	return 12 * time.Hour
}

type historyHostDTO struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
}

type historyDTO struct {
	Host                historyHostDTO   `json:"host"`
	Resolution          string           `json:"resolution"`
	From                time.Time        `json:"from"`
	To                  time.Time        `json:"to"`
	StepSeconds         int              `json:"step_seconds"`
	AvailableFrom       *time.Time       `json:"available_from"`
	DetailRetentionDays int              `json:"detail_retention_days"`
	Points              []metricPointDTO `json:"points"`
}

// hostHistory: GET /hosts/{id}/history?from=&to=[&format=csv][&series=metrics|disks] (viewer o admin, con su ambito).
func (a *API) hostHistory(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return invalidID()
	}
	// Siempre con el ambito del usuario: fuera de el es el mismo 404 que un host inexistente (tambien pa discos).
	host, err := a.store.HostByID(r.Context(), currentUser(r).Scope, id)
	if err != nil {
		return err
	}
	p, err := parseHistory(r, a.now(), host.EffectiveDetailDays)
	if err != nil {
		return err
	}
	if p.csv {
		return a.historyCSV(w, r, host, p)
	}

	var (
		points []store.MetricPoint
		first  *time.Time
		step   time.Duration
	)
	if p.resolution == "raw" {
		step = rawStep(p.to.Sub(p.from))
		points, first, err = a.store.DetailSeries(r.Context(), host.ID, p.from, p.to, step)
	} else {
		step = hourlyStep(p.to.Sub(p.from))
		// Los resumenes son horas enteras: se incluye la hora que contiene "from".
		points, first, err = a.store.MetricSeriesHourlyRange(r.Context(), host.ID, p.from.Truncate(time.Hour), p.to, step)
	}
	if err != nil {
		return err
	}
	dtos := make([]metricPointDTO, 0, len(points))
	for _, pt := range points {
		dtos = append(dtos, newMetricPointDTO(pt))
	}
	writeJSON(w, http.StatusOK, historyDTO{
		Host: historyHostDTO{ID: host.ID, Hostname: host.Hostname}, Resolution: p.resolution,
		From: p.from, To: p.to, StepSeconds: int(step.Seconds()), AvailableFrom: first,
		DetailRetentionDays: host.EffectiveDetailDays, Points: dtos,
	})
	return nil
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

const maxFilenameHostLen = 64

// historyFilename arma "metricas|discos-<host>-<desde>_<hasta>.csv" con el hostname saneado (el hostname solo
// se valida contra NUL: cualquier caracter fuera de [A-Za-z0-9._-] pasa a "_" y el largo se acota).
func historyFilename(p historyParams, hostname string) string {
	kind := "metricas"
	if p.disks {
		kind = "discos"
	}
	name := unsafeFileChars.ReplaceAllString(hostname, "_")
	if len(name) > maxFilenameHostLen {
		name = name[:maxFilenameHostLen]
	}
	return kind + "-" + name + "-" + p.from.Format(time.DateOnly) + "_" + p.to.Format(time.DateOnly) + ".csv"
}

// historyCSV exporta el rango en streaming. Cupo propio (un export por usuario y tope global) y limite de tasa
// por usuario; audita con host, rango y resolucion (nunca datos).
func (a *API) historyCSV(w http.ResponseWriter, r *http.Request, host store.HostDetail, p historyParams) error {
	u := currentUser(r)
	release, err := a.exports.acquire(u.ID)
	if err != nil {
		return err
	}
	defer release()
	if ok, wait := a.exportLimiter.allow(u.ID, a.now()); !ok {
		return rateLimited(wait)
	}

	series, resolution := "metrics", p.resolution
	header := rawCSVHeader
	stream := a.streamRawCSV
	switch {
	case p.disks:
		series, resolution, header, stream = "disks", "raw", diskCSVHeader, a.streamDiskCSV
	case p.resolution == "hourly":
		header, stream = hourlyCSVHeader(), a.streamHourlyCSV
	}
	ex := newCSVExport(w, historyFilename(p, host.Hostname), header)
	err = stream(r, host.ID, p, ex)
	if err == nil {
		err = ex.finish()
	}
	audit := func(outcome, reason string) {
		d := map[string]any{"from": p.from.Format(time.RFC3339), "to": p.to.Format(time.RFC3339),
			"resolution": resolution, "series": series, "rows": ex.rows}
		if reason != "" {
			d["reason"] = reason
		}
		e := actor(u)
		e.Event, e.Outcome, e.TargetType, e.TargetID = evHistoryExport, outcome, "host", host.ID
		a.audit(r, e, d)
	}
	switch {
	case err == nil:
		audit(store.OutcomeOK, "")
		return nil
	case ex.started:
		// Ya se envio el status: no se puede cambiar; se corta el stream y se deja constancia.
		a.logger.WarnContext(r.Context(), "exportacion de historial interrumpida", "host_id", host.ID, "rows", ex.rows, "err", err)
		audit(store.OutcomeFail, "aborted")
		return nil
	default:
		audit(store.OutcomeFail, "error")
		return err
	}
}

var (
	rawCSVHeader  = []string{"ts", "cpu_pct", "mem_used_mb", "mem_total_mb", "swap_used_mb", "swap_total_mb", "load1", "load5", "load15", "battery_pct", "battery_charging", "net_rx_bytes", "net_tx_bytes", "net_rx_bps", "net_tx_bps"}
	diskCSVHeader = []string{"ts", "mount", "used_mb", "total_mb", "used_pct"}
)

func hourlyCSVHeader() []string {
	return append([]string{"hour", "samples"}, store.HourlyColumns...)
}

// csvNum escribe un numero sin ceros de relleno (hasta 3 decimales); vacio si no hay dato.
func csvNum(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(round3(*v), 'f', -1, 64)
}

func csvI64(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}

func csvF32(v *float32) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(float64(*v), 'f', -1, 32)
}

func csvI32(v *int32) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(int64(*v), 10)
}

func (a *API) streamRawCSV(r *http.Request, hostID string, p historyParams, ex *csvExport) error {
	rates := store.RateTracker{MaxGap: historyRawGapSeconds}
	return a.store.DetailRange(r.Context(), hostID, p.from, p.to, func(s store.DetailSample) error {
		rx, tx := rates.Next(s)
		var bat, charging string
		if s.BatteryPct != nil {
			bat = strconv.Itoa(int(*s.BatteryPct))
		}
		if s.BatteryCharging != nil {
			charging = strconv.FormatBool(*s.BatteryCharging)
		}
		return ex.row(s.TS.UTC().Format(time.RFC3339), csvF32(s.CPUPct), csvI32(s.MemUsedMB), csvI32(s.MemTotalMB),
			csvI32(s.SwapUsedMB), csvI32(s.SwapTotalMB), csvF32(s.Load1), csvF32(s.Load5), csvF32(s.Load15),
			bat, charging, csvI64(s.NetRxBytes), csvI64(s.NetTxBytes), csvNum(rx), csvNum(tx))
	})
}

func (a *API) streamHourlyCSV(r *http.Request, hostID string, p historyParams, ex *csvExport) error {
	// Resumenes de horas enteras: incluye la hora que contiene "from".
	return a.store.HourlyRange(r.Context(), hostID, p.from.Truncate(time.Hour), p.to, func(h store.HourlyRow) error {
		cells := make([]string, 0, 2+len(h.Values))
		cells = append(cells, h.Hour.Format(time.RFC3339), strconv.Itoa(h.Samples))
		for _, v := range h.Values {
			cells = append(cells, csvNum(v))
		}
		return ex.row(cells...)
	})
}

func (a *API) streamDiskCSV(r *http.Request, hostID string, p historyParams, ex *csvExport) error {
	return a.store.DiskSampleRange(r.Context(), hostID, p.from, p.to, func(d store.DiskHistoryRow) error {
		pct := ""
		if d.TotalMB > 0 {
			v := float64(d.UsedMB) * 100 / float64(d.TotalMB)
			pct = csvNum(&v)
		}
		// mount viene del agente: es texto no confiable.
		return ex.row(d.TS.Format(time.RFC3339), csvSafe(d.Mount), strconv.Itoa(d.UsedMB), strconv.Itoa(d.TotalMB), pct)
	})
}
