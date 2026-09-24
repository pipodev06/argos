package agent

import (
	"context"
	"time"

	"argos/agent/internal/quiet"
	"argos/agent/internal/sender"
)

const (
	// DefaultDiskInterval es cada cuanto se reportan los discos.
	DefaultDiskInterval = 300 * time.Second
	// hostInfoRefresh: host_info se reenvia aunque no cambie, por si el server lo perdio.
	hostInfoRefresh = 24 * time.Hour
	// hostInfoChangeGap: un cambio (p. ej. de IP al cambiar de red) no genera mas de un envio en este lapso.
	hostInfoChangeGap = 5 * time.Minute
)

// schedule decide en que reportes van los datos ocasionales (discos y host_info).
// Se marca como enviado al armar el reporte: reintentar un envio fallido reenvia el
// mismo reporte (con sus datos), no vuelve a planificar. NO es seguro pa uso concurrente.
type schedule struct {
	diskEvery time.Duration
	diskSent  bool
	diskAt    time.Time

	hostSent *sender.HostInfo
	hostAt   time.Time
}

func newSchedule(diskEvery time.Duration) *schedule {
	if diskEvery <= 0 {
		diskEvery = DefaultDiskInterval
	}
	return &schedule{diskEvery: diskEvery}
}

func (s *schedule) diskDue(now time.Time) bool {
	return !s.diskSent || now.Sub(s.diskAt) >= s.diskEvery
}

// hostInfoDue: al arrancar, cuando cambia (maximo 1 vez cada hostInfoChangeGap) y cada hostInfoRefresh.
func (s *schedule) hostInfoDue(now time.Time, cur sender.HostInfo) bool {
	if s.hostSent == nil {
		return true
	}
	elapsed := now.Sub(s.hostAt)
	return elapsed >= hostInfoRefresh || (elapsed >= hostInfoChangeGap && !cur.Equal(*s.hostSent))
}

// attach agrega al reporte los discos y el host_info que toquen en este ciclo.
// Un fallo al leerlos se omite (con aviso por cambio de estado) y se reintenta en el ciclo siguiente.
func (s *schedule) attach(ctx context.Context, r *sender.Report, o Options, track *quiet.Tracker) {
	now := o.Now()
	if o.Disks != nil && s.diskDue(now) {
		disks, err := o.Disks.Disks(ctx)
		switch {
		case err != nil:
			if ctx.Err() == nil {
				track.Fail(ctx, "discos", err)
			}
		default:
			track.OK(ctx, "discos")
			r.Disks = disks // vacio = ausente; igual se marca hecho pa no reintentar cada ciclo
			s.diskSent, s.diskAt = true, now
		}
	}
	if o.HostInfo != nil {
		hi, err := o.HostInfo.HostInfo(ctx)
		switch {
		case err != nil:
			if ctx.Err() == nil {
				track.Fail(ctx, "host_info", err)
			}
		default:
			track.OK(ctx, "host_info")
			hi.AgentVersion = o.Version
			if s.hostInfoDue(now, hi) {
				r.HostInfo = &hi
				s.hostSent, s.hostAt = &hi, now
			}
		}
	}
}
