package checks

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// Limites de ARGOS_PROBE_ALLOW.
const (
	// DefaultProbeMaxTargets es el tope de IPs objetivo por defecto (ARGOS_PROBE_MAX_TARGETS).
	DefaultProbeMaxTargets = 256
	minProbeBits4          = 24  // mascara minima IPv4 por entrada (a lo sumo 256 IPs)
	minProbeBits6          = 120 // equivalente en IPv6 (a lo sumo 256 IPs)
)

// ProbePolicy es la allowlist local del colector ping (ARGOS_PROBE_ALLOW). Se decide
// al arrancar; el servidor no puede ampliarla. Vacia = colector desactivado.
type ProbePolicy struct {
	prefixes []netip.Prefix
}

// ParseProbePolicy lee una lista separada por comas de IPs y CIDRs. Cada CIDR debe
// tener al menos /24 (IPv4) o /120 (IPv6); la suma de IPs no puede pasar de
// maxTargets; ninguna entrada puede empezar en un rango siempre bloqueado.
func ParseProbePolicy(raw string, maxTargets int) (*ProbePolicy, error) {
	if maxTargets <= 0 {
		return nil, errors.New("el tope de objetivos debe ser mayor que 0")
	}
	p := &ProbePolicy{}
	total := 0
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		pfx, err := parseProbeEntry(part)
		if err != nil {
			return nil, err
		}
		if probeForbidden(pfx.Addr()) {
			return nil, fmt.Errorf("%q: rango siempre bloqueado (loopback, link-local, multicast, broadcast o no especificada)", part)
		}
		total += prefixSize(pfx)
		if total > maxTargets {
			return nil, fmt.Errorf("mas de %d IPs objetivo en total", maxTargets)
		}
		p.prefixes = append(p.prefixes, pfx)
	}
	return p, nil
}

func parseProbeEntry(s string) (netip.Prefix, error) {
	if pfx, err := netip.ParsePrefix(s); err == nil {
		if pfx.Addr().Zone() != "" {
			return netip.Prefix{}, fmt.Errorf("%q: no se admiten zonas", s)
		}
		addr := pfx.Addr().Unmap()
		bits := pfx.Bits()
		if pfx.Addr().Is4In6() {
			bits -= 96
		}
		floor := minProbeBits4
		if addr.Is6() {
			floor = minProbeBits6
		}
		if bits < floor {
			return netip.Prefix{}, fmt.Errorf("%q: mascara minima /%d", s, floor)
		}
		out, err := addr.Prefix(bits)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("%q: prefijo invalido", s)
		}
		return out, nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil || a.Zone() != "" {
		return netip.Prefix{}, fmt.Errorf("%q no es una IP ni un CIDR", s)
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}

func prefixSize(p netip.Prefix) int {
	return 1 << (p.Addr().BitLen() - p.Bits())
}

// probeForbidden son los destinos que jamas se sondean, aunque una entrada los cubra.
func probeForbidden(ip netip.Addr) bool {
	ip = ip.Unmap()
	return !ip.IsValid() || ip.Zone() != "" || ip.IsUnspecified() || ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() || ip.IsMulticast() ||
		ip == netip.AddrFrom4([4]byte{255, 255, 255, 255})
}

// Allows dice si ip (ya validada, sin DNS) puede sondearse. Una politica nil o vacia
// no permite nada.
func (p *ProbePolicy) Allows(ip netip.Addr) bool {
	if p == nil || probeForbidden(ip) {
		return false
	}
	ip = ip.Unmap()
	for _, pfx := range p.prefixes {
		if !pfx.Contains(ip) {
			continue
		}
		// La ultima IPv4 de un rango mayor que /32 es su broadcast dirigido.
		if ip.Is4() && pfx.Bits() < 32 && ip == lastAddr(pfx) {
			return false
		}
		return true
	}
	return false
}

// Enabled dice si hay al menos una entrada (vacia = colector desactivado).
func (p *ProbePolicy) Enabled() bool { return p != nil && len(p.prefixes) > 0 }

func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Addr().As4()
	host := 32 - p.Bits()
	for i := 3; i >= 0 && host > 0; i-- {
		n := min(host, 8)
		b[i] |= byte(1<<n - 1)
		host -= n
	}
	return netip.AddrFrom4(b)
}
