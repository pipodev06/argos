package notify

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
)

// Errores de destino no permitido. Los mensajes no incluyen la URL ni la IP.
var (
	ErrBlockedDestination = errors.New("destino no permitido")
	ErrURLInvalid         = errors.New("url invalida")
)

const (
	maxURLLen   = 2048
	defaultPort = "443"
)

// blockedPrefixes son rangos que nunca son un destino publico legitimo. Los rangos que netip ya clasifica
// (loopback, privadas RFC 1918/ULA, link-local incl. metadata 169.254.169.254, multicast, no especificada)
// se comprueban aparte en blockedIP.
var blockedPrefixes = mustPrefixes(
	"0.0.0.0/8",       // "esta red"
	"100.64.0.0/10",   // CGNAT
	"169.254.0.0/16",  // link-local y metadata de nube
	"192.0.0.0/24",    // asignaciones IETF
	"192.0.2.0/24",    // documentacion
	"198.18.0.0/15",   // benchmarking
	"198.51.100.0/24", // documentacion
	"203.0.113.0/24",  // documentacion
	"240.0.0.0/4",     // reservado y broadcast
	"::/96",           // IPv4-compatible (obsoleto)
	"64:ff9b::/96",    // NAT64
	"64:ff9b:1::/48",  // NAT64 de uso local
	"100::/64",        // descarte
	"2001::/32",       // Teredo
	"2001:db8::/32",   // documentacion
	"2002::/16",       // 6to4
	"fc00::/7",        // ULA (incluye fd00::/8)
	"fe80::/10",       // link-local
	"fec0::/10",       // site-local (obsoleto)
)

func mustPrefixes(list ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(list))
	for i, s := range list {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

// blockedIP dice si ip no debe alcanzarse jamas salvo allowlist. Las IPv4 mapeadas a IPv6 se desenvuelven
// primero: ::ffff:127.0.0.1 es loopback.
func blockedIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.Zone() != "" {
		return true
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Policy es la allowlist de destinos (ARGOS_WEBHOOK_ALLOW_HOSTS). Se decide al arrancar; no hay API que la cambie.
// Un destino permitido puede resolver a IPs bloqueadas y usar puertos distintos de 443. El cero es la politica
// estricta.
type Policy struct {
	hosts    map[string]bool
	prefixes []netip.Prefix
}

// ParsePolicy lee una lista separada por comas de hostnames exactos, IPs o CIDRs.
func ParsePolicy(raw string) (*Policy, error) {
	p := &Policy{hosts: map[string]bool{}}
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		if pfx, err := netip.ParsePrefix(part); err == nil {
			p.prefixes = append(p.prefixes, pfx.Masked())
			continue
		}
		if addr, err := netip.ParseAddr(part); err == nil {
			addr = addr.Unmap()
			p.prefixes = append(p.prefixes, netip.PrefixFrom(addr, addr.BitLen()))
			continue
		}
		if !validHostname(part) {
			return nil, fmt.Errorf("%q no es un hostname, IP ni CIDR", part)
		}
		p.hosts[part] = true
	}
	return p, nil
}

func validHostname(h string) bool {
	if h == "" || len(h) > 253 {
		return false
	}
	for _, r := range h {
		ok := r == '.' || r == '-' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}

func normHost(h string) string { return strings.TrimSuffix(strings.ToLower(h), ".") }

// hostAllowed: el hostname (o IP literal) de la URL esta en la allowlist.
func (p *Policy) hostAllowed(host string) bool {
	if p == nil {
		return false
	}
	host = normHost(host)
	if p.hosts[host] {
		return true
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return p.ipAllowed(addr)
	}
	return false
}

func (p *Policy) ipAllowed(ip netip.Addr) bool {
	if p == nil {
		return false
	}
	ip = ip.Unmap()
	for _, pfx := range p.prefixes {
		if pfx.Contains(ip) {
			return true
		}
	}
	return false
}

// checkIP acepta ip si no esta bloqueada o si el host (hostOK) o la propia IP estan en la allowlist.
func (p *Policy) checkIP(ip netip.Addr, hostOK bool) error {
	if !blockedIP(ip) || hostOK || p.ipAllowed(ip) {
		return nil
	}
	return ErrBlockedDestination
}

// control devuelve el Dialer.Control que valida la IP ya resuelta justo antes de conectar (ultima barrera:
// tambien cubre cambios de IP entre la validacion y la conexion).
func (p *Policy) control(hostOK bool) func(network, address string, c syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return ErrBlockedDestination
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return ErrBlockedDestination
		}
		if port != defaultPort && !hostOK && !p.ipAllowed(ip) {
			return ErrBlockedDestination
		}
		return p.checkIP(ip, hostOK)
	}
}

// ParseTarget valida la forma de una URL de webhook (sin resolver DNS): https, sin credenciales, host presente,
// puerto 443 salvo destinos de la allowlist. Devuelve la URL parseada. El error nunca contiene la URL.
func (p *Policy) ParseTarget(raw string) (*url.URL, error) {
	if raw == "" || len(raw) > maxURLLen {
		return nil, fmt.Errorf("%w: largo fuera de rango", ErrURLInvalid)
	}
	for _, r := range raw {
		if r < 0x21 || r == 0x7f {
			return nil, fmt.Errorf("%w: caracteres no permitidos", ErrURLInvalid)
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: no se puede interpretar", ErrURLInvalid)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("%w: el esquema debe ser https", ErrURLInvalid)
	}
	host := u.Hostname()
	if host == "" || u.User != nil || u.Fragment != "" {
		return nil, fmt.Errorf("%w: debe llevar host y no credenciales ni fragmento", ErrURLInvalid)
	}
	if port := u.Port(); port != "" && port != defaultPort && !p.hostAllowed(host) {
		return nil, fmt.Errorf("%w: solo se permite el puerto 443", ErrURLInvalid)
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if err := p.checkIP(addr, p.hostAllowed(host)); err != nil {
			return nil, err
		}
	}
	return u, nil
}
