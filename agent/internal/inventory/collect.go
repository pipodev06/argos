package inventory

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"syscall"

	psnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// ProcInfo es lo unico que se lee de un proceso: su nombre y su memoria residente.
type ProcInfo struct {
	Name string
	RSS  uint64
}

// Sources son las lecturas del SO que usan los colectores (inyectables en tests).
// Services nil = el modulo de servicios no esta disponible en este SO.
type Sources struct {
	Conns    func(ctx context.Context) ([]psnet.ConnectionStat, error)
	ProcName func(ctx context.Context, pid int32) (string, error)
	Procs    func(ctx context.Context) ([]ProcInfo, error)
	Services func(ctx context.Context) ([]Service, error)
	Updates  func(ctx context.Context) (Updates, error) // nil = modulo no disponible en este SO
}

// SystemSources devuelve las lecturas reales (gopsutil; servicios en Windows y Linux).
func SystemSources() Sources {
	return Sources{
		Conns: func(ctx context.Context) ([]psnet.ConnectionStat, error) {
			c, err := psnet.ConnectionsWithContext(ctx, "inet")
			if err != nil {
				return nil, fmt.Errorf("listando conexiones: %w", err)
			}
			return c, nil
		},
		ProcName: func(ctx context.Context, pid int32) (string, error) {
			p, err := process.NewProcessWithContext(ctx, pid)
			if err != nil {
				return "", fmt.Errorf("abriendo proceso: %w", err)
			}
			n, err := p.NameWithContext(ctx)
			if err != nil {
				return "", fmt.Errorf("nombre del proceso: %w", err)
			}
			return n, nil
		},
		Procs:    systemProcs,
		Services: servicesSource(),
	}
}

func systemProcs(ctx context.Context) ([]ProcInfo, error) {
	ps, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("listando procesos: %w", err)
	}
	out := make([]ProcInfo, 0, len(ps))
	for _, p := range ps {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("listando procesos: %w", err)
		}
		name, err := p.NameWithContext(ctx)
		if err != nil {
			continue // proceso terminado o protegido: no se reporta
		}
		info := ProcInfo{Name: name}
		if mi, err := p.MemoryInfoWithContext(ctx); err == nil && mi != nil {
			info.RSS = mi.RSS
		}
		out = append(out, info)
	}
	return out, nil
}

// collectPorts arma la foto de puertos: TCP en LISTEN y UDP sin conexion. El dueno se resuelve
// por PID; si no se puede (PID 0/4, proceso protegido, sin permisos) queda "desconocido".
func collectPorts(ctx context.Context, src Sources) (snapshot, error) {
	conns, err := src.Conns(ctx)
	if err != nil {
		return snapshot{}, err
	}
	names := map[int32]string{} // pid -> nombre ("" = no se pudo resolver)
	byKey := map[string]Port{}
	for _, c := range conns {
		proto := ""
		switch {
		case c.Type == syscall.SOCK_STREAM && c.Status == "LISTEN":
			proto = "tcp"
		case c.Type == syscall.SOCK_DGRAM && c.Raddr.Port == 0:
			proto = "udp"
		default:
			continue
		}
		addr, err := netip.ParseAddr(c.Laddr.IP)
		if err != nil || c.Laddr.Port == 0 || c.Laddr.Port > 65535 {
			continue
		}
		p := Port{Proto: proto, Bind: addr.Unmap().WithZone("").String(), Port: int(c.Laddr.Port)}
		if c.Pid > 0 {
			name, seen := names[c.Pid]
			if !seen {
				n, err := src.ProcName(ctx, c.Pid)
				if err == nil {
					name = clean(n, maxNameLen)
				}
				names[c.Pid] = name
			}
			if name != "" {
				p.PID, p.Proc, p.OwnerKnown = c.Pid, name, true
			}
		}
		key := fmt.Sprintf("%s|%s|%d", p.Proto, p.Bind, p.Port)
		if old, dup := byKey[key]; !dup || (!old.OwnerKnown && p.OwnerKnown) {
			byKey[key] = p
		}
	}
	items := make([]Port, 0, len(byKey))
	for _, p := range byKey {
		items = append(items, p)
	}
	return finish(ModulePorts, items, false, maxPorts, portLine)
}

// collectServices arma la foto de servicios del SO.
func collectServices(ctx context.Context, src Sources) (snapshot, error) {
	raw, err := src.Services(ctx)
	if err != nil {
		return snapshot{}, err
	}
	byName := map[string]Service{}
	for _, s := range raw {
		s.Name = clean(s.Name, maxNameLen)
		if s.Name == "" {
			continue
		}
		s.Display = clean(s.Display, maxDisplayLen)
		byName[s.Name] = s
	}
	items := make([]Service, 0, len(byName))
	for _, s := range byName {
		items = append(items, s)
	}
	return finish(ModuleServices, items, false, maxServices, serviceLine)
}

// collectProcesses agrupa por nombre y se queda con los maxProcesses de mayor RSS.
func collectProcesses(ctx context.Context, src Sources) (snapshot, error) {
	raw, err := src.Procs(ctx)
	if err != nil {
		return snapshot{}, err
	}
	groups := map[string]*Process{}
	for _, p := range raw {
		name := clean(p.Name, maxNameLen)
		if name == "" {
			continue
		}
		g := groups[name]
		if g == nil {
			g = &Process{Name: name}
			groups[name] = g
		}
		g.Count++
		g.RSSBytes += p.RSS
	}
	all := make([]Process, 0, len(groups))
	for _, g := range groups {
		all = append(all, *g)
	}
	slices.SortFunc(all, func(a, b Process) int {
		if a.RSSBytes != b.RSSBytes {
			if a.RSSBytes > b.RSSBytes {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	truncated := len(all) > maxProcesses
	if truncated {
		all = all[:maxProcesses]
	}
	return finish(ModuleProcesses, all, truncated, maxProcesses, processLine)
}
