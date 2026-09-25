package httpapi

import (
	"fmt"
	"math"
	"net/netip"
	"time"

	"argos/server/internal/store"
)

// Limites de los campos nuevos del reporte del agente.
const (
	maxDisks        = 32
	maxMountLen     = 128
	maxInfoTextLen  = 128
	maxAgentVersion = 32
	maxHostIPs      = 16
	maxCPUCores     = 4096      // SMALLINT en la BD (CHECK 1..4096)
	maxCPUThreads   = 8192      // SMALLINT en la BD (CHECK 1..8192)
	maxLoad         = 1e6       // REAL admite mucho mas; esto es un tope de cordura
	minBootTimeUnix = 946684800 // 2000-01-01: una fecha anterior es basura
)

type diskPayload struct {
	Mount   string `json:"mount"`
	UsedMB  int64  `json:"used_mb"`
	TotalMB int64  `json:"total_mb"`
}

type hostInfoPayload struct {
	BootTime      int64  `json:"boot_time"`
	CPUModel      string `json:"cpu_model"`
	CPUCores      int    `json:"cpu_cores"`
	CPUThreads    int    `json:"cpu_threads"`
	OSName        string `json:"os_name"`
	OSVersion     string `json:"os_version"`
	KernelVersion string `json:"kernel_version"`
	AgentVersion  string `json:"agent_version"`
	// Privilege: nivel con el que corre el agente ("user" | "elevated"); vacio = agente que aun no lo reporta.
	Privilege string   `json:"privilege"`
	IPs       []string `json:"ips"`
}

// validateExtras valida swap, load y bateria y los copia al sample (ausente = nil). Cada campo es opcional
// por separado.
func validateExtras(m *metricsPayload, dst *store.Sample) error {
	if m.Swap != nil {
		var err error
		if dst.SwapUsedMB, err = optMB("metrics.swap.used_mb", m.Swap.UsedMB); err != nil {
			return err
		}
		if dst.SwapTotalMB, err = optMB("metrics.swap.total_mb", m.Swap.TotalMB); err != nil {
			return err
		}
	}
	if m.Load != nil {
		var err error
		if dst.Load1, err = optLoad("metrics.load.l1", m.Load.L1); err != nil {
			return err
		}
		if dst.Load5, err = optLoad("metrics.load.l5", m.Load.L5); err != nil {
			return err
		}
		if dst.Load15, err = optLoad("metrics.load.l15", m.Load.L15); err != nil {
			return err
		}
	}
	if b := m.Battery; b != nil {
		if b.Pct != nil {
			if *b.Pct < 0 || *b.Pct > 100 {
				return badRequest("metrics.battery.pct fuera de rango 0-100")
			}
			pct := int(*b.Pct)
			dst.BatteryPct = &pct
		}
		dst.BatteryCharging = b.Charging
	}
	return nil
}

// optMB valida un tamano en MB opcional: no negativo y dentro de int32 (columna INT).
func optMB(field string, v *int64) (*int, error) {
	if v == nil {
		return nil, nil
	}
	if *v < 0 || *v > math.MaxInt32 {
		return nil, badRequest("%s fuera de rango", field)
	}
	n := int(*v)
	return &n, nil
}

// optLoad valida un load average opcional: finito, no negativo y acotado.
func optLoad(field string, v *float64) (*float64, error) {
	if v == nil {
		return nil, nil
	}
	if math.IsNaN(*v) || *v < 0 || *v > maxLoad {
		return nil, badRequest("%s fuera de rango 0-%g", field, maxLoad)
	}
	return v, nil
}

// validateDisks valida la lista de discos (vacia = ausente: se devuelve nil).
func validateDisks(in []diskPayload) ([]store.DiskSample, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > maxDisks {
		return nil, badRequest("maximo %d disks", maxDisks)
	}
	seen := make(map[string]bool, len(in))
	out := make([]store.DiskSample, 0, len(in))
	for i, d := range in {
		field := fmt.Sprintf("disks[%d].mount", i)
		if d.Mount == "" {
			return nil, badRequest("%s requerido", field)
		}
		if err := checkOptionalText(field, d.Mount, maxMountLen); err != nil {
			return nil, err
		}
		if seen[d.Mount] {
			return nil, badRequest("disks[%d].mount repetido: %s", i, d.Mount)
		}
		seen[d.Mount] = true
		if d.TotalMB <= 0 || d.TotalMB > math.MaxInt32 {
			return nil, badRequest("disks[%d].total_mb debe estar entre 1 y %d", i, math.MaxInt32)
		}
		if d.UsedMB < 0 || d.UsedMB > d.TotalMB {
			return nil, badRequest("disks[%d].used_mb debe estar entre 0 y total_mb", i)
		}
		out = append(out, store.DiskSample{Mount: d.Mount, UsedMB: int(d.UsedMB), TotalMB: int(d.TotalMB)})
	}
	return out, nil
}

// validateHostInfo valida la info estatica (nil = ausente). Textos vacios y numeros en cero son "sin dato".
func validateHostInfo(in *hostInfoPayload, now time.Time) (*store.HostInfoUpdate, error) {
	if in == nil {
		return nil, nil
	}
	texts := []struct {
		field string
		v     string
		max   int
	}{
		{"host_info.cpu_model", in.CPUModel, maxInfoTextLen},
		{"host_info.os_name", in.OSName, maxInfoTextLen},
		{"host_info.os_version", in.OSVersion, maxInfoTextLen},
		{"host_info.kernel_version", in.KernelVersion, maxInfoTextLen},
		{"host_info.agent_version", in.AgentVersion, maxAgentVersion},
	}
	for _, t := range texts {
		if err := checkOptionalText(t.field, t.v, t.max); err != nil {
			return nil, err
		}
	}
	if in.Privilege != "" && in.Privilege != "user" && in.Privilege != "elevated" {
		return nil, badRequest("host_info.privilege debe ser user o elevated")
	}
	out := &store.HostInfoUpdate{
		CPUModel: in.CPUModel, OSName: in.OSName, OSVersion: in.OSVersion,
		KernelVersion: in.KernelVersion, AgentVersion: in.AgentVersion, Privilege: in.Privilege,
	}

	if in.CPUCores < 0 || in.CPUCores > maxCPUCores {
		return nil, badRequest("host_info.cpu_cores fuera de rango 1-%d", maxCPUCores)
	}
	if in.CPUThreads < 0 || in.CPUThreads > maxCPUThreads {
		return nil, badRequest("host_info.cpu_threads fuera de rango 1-%d", maxCPUThreads)
	}
	if in.CPUCores > 0 {
		out.CPUCores = &in.CPUCores
	}
	if in.CPUThreads > 0 {
		out.CPUThreads = &in.CPUThreads
	}

	if in.BootTime != 0 {
		if in.BootTime < minBootTimeUnix || in.BootTime > now.Add(maxTimestampSkew).Unix() {
			return nil, badRequest("host_info.boot_time fuera de rango")
		}
		bt := time.Unix(in.BootTime, 0)
		out.BootTime = &bt
	}

	if len(in.IPs) > maxHostIPs {
		return nil, badRequest("host_info.ips admite como maximo %d direcciones", maxHostIPs)
	}
	for _, raw := range in.IPs {
		ip, err := netip.ParseAddr(raw)
		if err != nil || ip.Zone() != "" {
			return nil, badRequest("host_info.ips contiene una direccion invalida")
		}
		out.IPs = append(out.IPs, ip.Unmap())
	}
	return out, nil
}
