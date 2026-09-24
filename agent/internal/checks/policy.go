package checks

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// errBlocked: el destino no esta permitido por la politica local. No incluye IP ni host.
var errBlocked = errors.New("destino no permitido por la politica local")

type policyEntry struct {
	host     string       // hostname exacto (vacio si es prefijo)
	prefix   netip.Prefix // IP o CIDR
	isPrefix bool
	port     uint16 // 0 = cualquier puerto
}

// Policy es la allowlist local (ARGOS_CHECK_ALLOW). Se decide al arrancar; el servidor
// no puede ampliarla. El cero (o vacia) es el defecto: loopback + IPs propias del host.
type Policy struct {
	entries []policyEntry
}

// ParsePolicy lee una lista separada por comas de hostnames exactos, IPs y CIDRs,
// cada uno con ":puerto" opcional ([::1]:80 pa IPv6 con puerto).
func ParsePolicy(raw string) (*Policy, error) {
	p := &Policy{}
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		e, err := parseEntry(part)
		if err != nil {
			return nil, err
		}
		p.entries = append(p.entries, e)
	}
	return p, nil
}

func parseEntry(s string) (policyEntry, error) {
	if pfx, err := netip.ParsePrefix(s); err == nil {
		return policyEntry{prefix: pfx.Masked(), isPrefix: true}, nil
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return hostEntry(a, 0), nil
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return hostEntry(ap.Addr(), ap.Port()), nil
	}
	host, portStr, found := strings.Cut(s, ":")
	var port uint16
	if found {
		n, err := strconv.ParseUint(portStr, 10, 16)
		if err != nil || n == 0 {
			return policyEntry{}, fmt.Errorf("%q: puerto invalido", s)
		}
		port = uint16(n)
	}
	if pfx, err := netip.ParsePrefix(host); err == nil {
		return policyEntry{prefix: pfx.Masked(), isPrefix: true, port: port}, nil
	}
	if !validHostname(host) {
		return policyEntry{}, fmt.Errorf("%q no es un hostname, IP ni CIDR (con :puerto opcional)", s)
	}
	return policyEntry{host: host, port: port}, nil
}

func hostEntry(a netip.Addr, port uint16) policyEntry {
	a = a.Unmap()
	return policyEntry{prefix: netip.PrefixFrom(a, a.BitLen()), isPrefix: true, port: port}
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

// allows dice si se puede conectar a ip:port (resuelta desde host). Siempre se permite
// loopback y las IPs propias (own); el resto exige una entrada de la allowlist.
func (p *Policy) allows(host string, ip netip.Addr, port uint16, own []netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.Zone() != "" {
		return false
	}
	if ip.IsLoopback() || slices.Contains(own, ip) {
		return true
	}
	if p == nil {
		return false
	}
	host = normHost(host)
	for _, e := range p.entries {
		if e.port != 0 && e.port != port {
			continue
		}
		if (e.isPrefix && e.prefix.Contains(ip)) || (!e.isPrefix && e.host == host) {
			return true
		}
	}
	return false
}

// control es el Dialer.Control que valida la IP ya resuelta justo antes de conectar
// (ultima barrera, tambien cubre un cambio de IP entre la validacion y la conexion).
func (p *Policy) control(host string, own []netip.Addr) func(network, address string, c syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		h, portStr, err := net.SplitHostPort(address)
		if err != nil {
			return errBlocked
		}
		ip, err := netip.ParseAddr(h)
		if err != nil {
			return errBlocked
		}
		port, err := strconv.ParseUint(portStr, 10, 16)
		if err != nil || !p.allows(host, ip, uint16(port), own) {
			return errBlocked
		}
		return nil
	}
}
