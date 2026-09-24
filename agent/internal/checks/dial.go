package checks

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
)

// dialer conecta aplicando la politica con pinning: resuelve UNA vez, valida TODAS
// las IPs resueltas y conecta solo a esas (Control las vuelve a validar).
type dialer struct {
	policy  *Policy
	resolve func(ctx context.Context, host string) ([]netip.Addr, error)
	own     func() []netip.Addr
}

func (d *dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errBlocked
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, errBlocked
	}
	var ips []netip.Addr
	if ip, perr := netip.ParseAddr(host); perr == nil {
		ips = []netip.Addr{ip}
	} else if ips, err = d.resolve(ctx, host); err != nil {
		return nil, fmt.Errorf("resolviendo destino: %w", err)
	}
	if len(ips) == 0 {
		return nil, &net.DNSError{Err: "sin direcciones", IsNotFound: true}
	}
	own := d.own()
	for _, ip := range ips {
		if !d.policy.allows(host, ip, uint16(port), own) {
			return nil, errBlocked
		}
	}
	var lastErr error
	for _, ip := range ips {
		nd := net.Dialer{Control: d.policy.control(host, own)}
		conn, derr := nd.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), portStr))
		if derr == nil {
			return conn, nil
		}
		lastErr = derr
		if ctx.Err() != nil {
			break
		}
	}
	return nil, fmt.Errorf("conectando: %w", lastErr)
}

func defaultResolve(ctx context.Context, host string) ([]netip.Addr, error) {
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("lookup: %w", err)
	}
	return ips, nil
}

// ownIPs lista las IPs de las interfaces del host (se consulta en cada chequeo: cambian).
func ownIPs() []netip.Addr {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	out := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			if ip, ok := netip.AddrFromSlice(ipn.IP); ok {
				out = append(out, ip.Unmap())
			}
		}
	}
	return out
}
