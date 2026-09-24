package httpapi

import (
	"math"
	"net/http"
	"time"

	"argos/server/internal/store"
)

// metricRange es un rango de consulta permitido y el ancho de bucket que le toca:
// acota la respuesta a duration/step puntos (+1 por el bucket parcial).
type metricRange struct {
	duration time.Duration
	step     time.Duration
	// hourly: lee los resumenes por hora (metrics_hourly) en vez de las muestras crudas.
	// Como solo hay resumen de horas cerradas, la hora en curso no aparece en estos rangos.
	hourly bool
}

// metricRanges es la allowlist estricta de ?range=; cualquier otro valor es 400.
// 1h..7d leen el crudo (contrato original); 30d, 90d y 1y leen los resumenes por hora.
var metricRanges = map[string]metricRange{
	"1h":  {time.Hour, 30 * time.Second, false},
	"6h":  {6 * time.Hour, 2 * time.Minute, false},
	"24h": {24 * time.Hour, 10 * time.Minute, false},
	"7d":  {7 * 24 * time.Hour, time.Hour, false},
	"30d": {30 * 24 * time.Hour, time.Hour, true},
	"90d": {90 * 24 * time.Hour, 3 * time.Hour, true},
	"1y":  {365 * 24 * time.Hour, 12 * time.Hour, true},
}

const defaultMetricRange = "1h"

type hostDetailDTO struct {
	ID              string       `json:"id"`
	Hostname        string       `json:"hostname"`
	OS              string       `json:"os"`
	Arch            string       `json:"arch"`
	Status          string       `json:"status"`
	LastSeenAt      *time.Time   `json:"last_seen_at"`
	StatusChangedAt time.Time    `json:"status_changed_at"`
	CreatedAt       time.Time    `json:"created_at"`
	Group           *groupRefDTO `json:"group"`
	Tags            []string     `json:"tags"`
}

// hostInfoDTO es la info estatica del host (host_info) mas la memoria total. Todo es null si el agente no la manda.
type hostInfoDTO struct {
	BootTime      *time.Time `json:"boot_time"`
	CPUModel      *string    `json:"cpu_model"`
	CPUCores      *int       `json:"cpu_cores"`
	CPUThreads    *int       `json:"cpu_threads"`
	OSName        *string    `json:"os_name"`
	OSVersion     *string    `json:"os_version"`
	KernelVersion *string    `json:"kernel_version"`
	AgentVersion  *string    `json:"agent_version"`
	IPs           []string   `json:"ips"`
	MemTotalMB    *int       `json:"mem_total_mb"`
}

func toHostInfoDTO(i store.HostInfo) hostInfoDTO {
	dto := hostInfoDTO{
		CPUModel: i.CPUModel, CPUCores: i.CPUCores, CPUThreads: i.CPUThreads, OSName: i.OSName, OSVersion: i.OSVersion,
		KernelVersion: i.KernelVersion, AgentVersion: i.AgentVersion, MemTotalMB: i.MemTotalMB, IPs: make([]string, 0, len(i.IPs)),
	}
	if i.BootTime != nil {
		t := i.BootTime.UTC()
		dto.BootTime = &t
	}
	for _, ip := range i.IPs {
		dto.IPs = append(dto.IPs, ip.String())
	}
	return dto
}

// diskDTO es la ultima muestra de un montaje.
type diskDTO struct {
	Mount   string    `json:"mount"`
	UsedMB  int       `json:"used_mb"`
	TotalMB int       `json:"total_mb"`
	TS      time.Time `json:"ts"`
}

// metricPointDTO: cpu con 1 decimal; memoria, swap, bateria y tasas de red enteras; carga con 2 decimales.
// Los campos nuevos (swap, load, bateria) son null si no hay dato.
type metricPointDTO struct {
	TS          time.Time `json:"ts"`
	CPUPct      *float64  `json:"cpu_pct"`
	MemUsedMB   *int64    `json:"mem_used_mb"`
	MemTotalMB  *int64    `json:"mem_total_mb"`
	NetRxBps    *int64    `json:"net_rx_bps"`
	NetTxBps    *int64    `json:"net_tx_bps"`
	SwapUsedMB  *int64    `json:"swap_used_mb"`
	SwapTotalMB *int64    `json:"swap_total_mb"`
	Load1       *float64  `json:"load1"`
	Load5       *float64  `json:"load5"`
	Load15      *float64  `json:"load15"`
	BatteryPct  *int64    `json:"battery_pct"`
}

// latestDTO es el ultimo punto: lo de metricPointDTO mas si la bateria esta cargando.
type latestDTO struct {
	metricPointDTO
	BatteryCharging *bool `json:"battery_charging"`
}

func newMetricPointDTO(p store.MetricPoint) metricPointDTO {
	return metricPointDTO{
		TS:          p.TS.UTC(),
		CPUPct:      round1(p.CPUPct),
		MemUsedMB:   roundInt(p.MemUsedMB),
		MemTotalMB:  roundInt(p.MemTotalMB),
		NetRxBps:    roundInt(p.NetRxBps),
		NetTxBps:    roundInt(p.NetTxBps),
		SwapUsedMB:  roundInt(p.SwapUsedMB),
		SwapTotalMB: roundInt(p.SwapTotalMB),
		Load1:       round2(p.Load1),
		Load5:       round2(p.Load5),
		Load15:      round2(p.Load15),
		BatteryPct:  roundInt(p.BatteryPct),
	}
}

func round1(v *float64) *float64 {
	if v == nil {
		return nil
	}
	r := math.Round(*v*10) / 10
	return &r
}

func round2(v *float64) *float64 {
	if v == nil {
		return nil
	}
	r := math.Round(*v*100) / 100
	return &r
}

func roundInt(v *float64) *int64 {
	if v == nil {
		return nil
	}
	r := int64(math.Round(*v))
	return &r
}

// getHost devuelve la ficha del host (con grupo y etiquetas), su info estatica, su ultima muestra y la ultima
// muestra de cada disco (viewer o admin).
func (a *API) getHost(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return invalidID()
	}
	ctx := r.Context()
	host, err := a.store.HostByID(ctx, id)
	if err != nil {
		return err
	}
	last, err := a.store.LatestMetrics(ctx, id)
	if err != nil {
		return err
	}
	disks, err := a.store.HostDisks(ctx, id, a.now())
	if err != nil {
		return err
	}

	var latest *latestDTO
	if last != nil {
		latest = &latestDTO{metricPointDTO: newMetricPointDTO(*last), BatteryCharging: last.BatteryCharging}
	}
	diskDTOs := make([]diskDTO, 0, len(disks))
	for _, d := range disks {
		diskDTOs = append(diskDTOs, diskDTO{Mount: d.Mount, UsedMB: d.UsedMB, TotalMB: d.TotalMB, TS: d.TS.UTC()})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"host": hostDetailDTO{
			ID: host.ID, Hostname: host.Hostname, OS: host.OS, Arch: host.Arch, Status: host.Status,
			LastSeenAt: utcPtr(host.LastSeenAt), StatusChangedAt: host.StatusChangedAt.UTC(), CreatedAt: host.CreatedAt.UTC(),
			Group: toGroupRefDTO(host.Group), Tags: nonNilTags(host.Tags),
		},
		"info":   toHostInfoDTO(host.Info),
		"latest": latest,
		"disks":  diskDTOs,
	})
	return nil
}

// hostMetrics devuelve la serie de tiempo del host (viewer o admin).
func (a *API) hostMetrics(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	if !isUUID(id) {
		return invalidID()
	}
	rangeName := r.URL.Query().Get("range")
	if rangeName == "" {
		rangeName = defaultMetricRange
	}
	rng, ok := metricRanges[rangeName]
	if !ok {
		return &clientError{status: http.StatusBadRequest, code: "invalid_range", msg: "range debe ser 1h, 6h, 24h, 7d, 30d, 90d o 1y"}
	}

	series := a.store.MetricSeries
	if rng.hourly {
		series = a.store.MetricSeriesHourly
	}
	points, err := series(r.Context(), id, a.now().Add(-rng.duration), rng.step)
	if err != nil {
		return err
	}
	dtos := make([]metricPointDTO, 0, len(points))
	for _, p := range points {
		dtos = append(dtos, newMetricPointDTO(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"range":        rangeName,
		"step_seconds": int(rng.step.Seconds()),
		"points":       dtos,
	})
	return nil
}

func invalidID() error {
	return &clientError{status: http.StatusBadRequest, code: "invalid_id", msg: "id de host invalido"}
}
