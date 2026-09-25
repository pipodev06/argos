package checks

import (
	"context"
	"errors"
	"net/netip"
	"time"
)

// MaxPingConcurrent es el tope de pings en vuelo a la vez (semaforo propio: no
// compite con el de los demas chequeos).
const MaxPingConcurrent = 16

var (
	// ErrPingNoReply: el objetivo no respondio (timeout, inalcanzable o error de recepcion).
	ErrPingNoReply = errors.New("sin respuesta al ping")
	// ErrPingUnavailable: este camino no puede sondear aqui (sin permisos, sin binario, IPv6...).
	ErrPingUnavailable = errors.New("ping no disponible por este camino")
)

// Pinger envia UN echo ICMP a ip y devuelve el RTT. El plazo lo marca ctx.
// Devuelve ErrPingNoReply o ErrPingUnavailable (envueltos si hace falta).
type Pinger interface {
	Ping(ctx context.Context, ip netip.Addr) (time.Duration, error)
}

// chainPinger prueba cada camino en orden: pasa al siguiente solo si el anterior
// no esta disponible (nunca tras un "sin respuesta").
type chainPinger []Pinger

func (c chainPinger) Ping(ctx context.Context, ip netip.Addr) (time.Duration, error) {
	for _, p := range c {
		rtt, err := p.Ping(ctx, ip)
		if !errors.Is(err, ErrPingUnavailable) {
			return rtt, err
		}
	}
	return 0, ErrPingUnavailable
}

// newSystemPinger: ICMP sin privilegios del SO y, como ultimo recurso, el `ping` del SO.
func newSystemPinger() Pinger {
	var c chainPinger
	if n := newNativePinger(); n != nil {
		c = append(c, n)
	}
	return append(c, newExecPinger())
}

// probePing valida el objetivo contra la politica y hace un intento.
func (r *Runner) probePing(ctx context.Context, c Check) outcome {
	ip, err := netip.ParseAddr(c.Target)
	if err != nil || ip.Zone() != "" {
		return unknown(DetailUnsupported)
	}
	ip = ip.Unmap()
	if !r.probeAllow.Allows(ip) {
		return unknown(DetailBlocked)
	}
	rtt, err := r.pinger.Ping(ctx, ip)
	switch {
	case err == nil:
		return latencyOutcome(rtt.Milliseconds(), c.WarnMS, c.CritMS)
	case errors.Is(err, ErrPingUnavailable):
		return unknown(DetailUnsupported)
	default:
		return outcome{status: StatusDown, detail: DetailTimeout}
	}
}
