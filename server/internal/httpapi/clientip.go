package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// clientIP devuelve la IP del cliente. Sale de RemoteAddr; solo si esa conexion
// viene de un proxy de confianza (ARGOS_TRUSTED_PROXIES) se lee X-Forwarded-For,
// recorrido de derecha a izquierda hasta el primer salto que no sea de confianza
// (los saltos de la izquierda los controla el cliente y se pueden falsificar).
// Devuelve la direccion cero si RemoteAddr no se puede interpretar.
func (a *API) clientIP(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remote, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	remote = remote.Unmap()
	if !a.isTrustedProxy(remote) {
		return remote
	}

	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return remote // cabecera malformada: nos quedamos con el proxy
		}
		hop = hop.Unmap()
		if !a.isTrustedProxy(hop) {
			return hop
		}
	}
	return remote
}

func (a *API) isTrustedProxy(ip netip.Addr) bool {
	for _, p := range a.opts.TrustedProxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// addrPtr devuelve nil pa una IP invalida (se guarda como NULL).
func addrPtr(ip netip.Addr) *netip.Addr {
	if !ip.IsValid() {
		return nil
	}
	return &ip
}
