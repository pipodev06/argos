package collector

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"

	"argos/agent/internal/sender"
)

const (
	// maxIPs acota las IPs por reporte.
	maxIPs = 16
	// staticInfoTTL: CPU, SO y arranque casi no cambian; leerlos cuesta (en Windows
	// pasa por WMI), asi que se cachean. Las IPs se leen siempre (es barato).
	staticInfoTTL = time.Hour
)

// virtualIfacePrefixes son interfaces de puentes/contenedores/hipervisores: sus IPs
// no identifican al equipo en la red y cambian solas (docker, WSL, VMs).
var virtualIfacePrefixes = []string{
	"docker", "br-", "veth", "virbr", "cni", "flannel", "cali", "lxc", "podman",
	"vboxnet", "vmnet", "vmware", "virtualbox", "vethernet", "awdl", "llw",
}

// ifaceAddrs es una interfaz de red con sus direcciones (lectura separada del filtrado).
type ifaceAddrs struct {
	Name  string
	Flags net.Flags
	Addrs []netip.Addr
}

// HostInfo devuelve los datos del host. Las IPs se leen en cada llamada; el resto
// se cachea staticInfoTTL. Un dato que no se pudo leer queda ausente (nunca un
// valor inventado); solo falla si no hay nada que informar.
func (s *System) HostInfo(ctx context.Context) (sender.HostInfo, error) {
	if !s.staticDone || s.now().Sub(s.staticAt) >= staticInfoTTL {
		info, errs := s.staticInfo(ctx)
		for _, err := range errs {
			s.quiet.Fail(ctx, "host_info", err)
		}
		if len(errs) == 0 {
			s.quiet.OK(ctx, "host_info")
		}
		s.static, s.staticAt, s.staticDone = info, s.now(), true
	}
	info := s.static
	ifs, err := s.interfaces()
	if s.optionalOK(ctx, "ips", err) {
		info.IPs = usableIPs(ifs)
	}
	if info.BootTime == 0 && info.CPUModel == "" && info.OSName == "" && len(info.IPs) == 0 {
		return sender.HostInfo{}, errors.New("no se pudo leer ningun dato del host")
	}
	return info, nil
}

// usableIPs deja las direcciones utiles pa identificar el equipo: unicast global o
// privada, sin loopback, link-local, multicast ni interfaces caidas o virtuales.
// Orden estable (IPv4 primero) y acotado a maxIPs.
func usableIPs(ifs []ifaceAddrs) []string {
	seen := map[netip.Addr]struct{}{}
	var ips []netip.Addr
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 || isVirtualIface(i.Name) {
			continue
		}
		for _, a := range i.Addrs {
			a = a.Unmap().WithZone("")
			if !a.IsValid() || !a.IsGlobalUnicast() || a.IsLinkLocalUnicast() {
				continue
			}
			if _, dup := seen[a]; dup {
				continue
			}
			seen[a] = struct{}{}
			ips = append(ips, a)
		}
	}
	slices.SortFunc(ips, netip.Addr.Compare)
	if len(ips) > maxIPs {
		ips = ips[:maxIPs]
	}
	out := make([]string, len(ips))
	for i, a := range ips {
		out[i] = a.String()
	}
	return out
}

func isVirtualIface(name string) bool {
	n := strings.ToLower(name)
	for _, p := range virtualIfacePrefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

// readInterfaces lista las interfaces con sus direcciones (biblioteca estandar).
func readInterfaces() ([]ifaceAddrs, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("listando interfaces: %w", err)
	}
	out := make([]ifaceAddrs, 0, len(ifs))
	for _, i := range ifs {
		addrs, err := i.Addrs()
		if err != nil {
			continue // una interfaz que desaparece a mitad de lectura no impide las demas
		}
		ia := ifaceAddrs{Name: i.Name, Flags: i.Flags}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if addr, ok := netip.AddrFromSlice(ip); ok {
				ia.Addrs = append(ia.Addrs, addr)
			}
		}
		out = append(out, ia)
	}
	return out, nil
}

// readStaticInfo lee lo que casi no cambia: arranque, SO, kernel y CPU (gopsutil).
// Devuelve lo que pudo leer y los errores de lo que no.
func readStaticInfo(ctx context.Context) (sender.HostInfo, []error) {
	var info sender.HostInfo
	var errs []error

	if h, err := host.InfoWithContext(ctx); err != nil {
		errs = append(errs, fmt.Errorf("leyendo datos del SO: %w", err))
	} else {
		info.BootTime = int64(min(h.BootTime, 1<<62)) //nolint:gosec // acotado arriba
		info.OSName = osName(h.Platform, h.OS)
		info.OSVersion = h.PlatformVersion
		info.KernelVersion = h.KernelVersion
	}
	if cs, err := cpu.InfoWithContext(ctx); err != nil {
		errs = append(errs, fmt.Errorf("leyendo modelo de cpu: %w", err))
	} else if len(cs) > 0 {
		info.CPUModel = strings.TrimSpace(cs[0].ModelName)
	}
	if n, err := cpu.CountsWithContext(ctx, false); err != nil {
		errs = append(errs, fmt.Errorf("contando nucleos: %w", err))
	} else {
		info.CPUCores = max(n, 0)
	}
	if n, err := cpu.CountsWithContext(ctx, true); err != nil {
		errs = append(errs, fmt.Errorf("contando hilos: %w", err))
	} else {
		info.CPUThreads = max(n, 0)
	}
	return info, errs
}

// osName prefiere el nombre de plataforma de gopsutil ("Microsoft Windows 11 Pro",
// "ubuntu") y cae al SO; en macOS gopsutil da "darwin".
func osName(platform, goos string) string {
	name := strings.TrimSpace(platform)
	if name == "" {
		name = strings.TrimSpace(goos)
	}
	if strings.EqualFold(name, "darwin") {
		return "macOS"
	}
	return name
}
