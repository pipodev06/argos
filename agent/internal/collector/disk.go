package collector

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/shirou/gopsutil/v4/disk"

	"argos/agent/internal/sender"
)

const (
	// maxDisks acota los montajes por reporte.
	maxDisks = 16
	// maxMountLen acota el largo del punto de montaje que se manda al server.
	maxMountLen = 255
)

// skipFSTypes son sistemas de archivos que no representan almacenamiento del
// equipo: virtuales/del kernel, imagenes de solo lectura, opticos y de red.
// Los de red se excluyen tambien porque un montaje caido puede colgar la consulta
// de uso (statfs) indefinidamente y bloquear al agente.
var skipFSTypes = map[string]struct{}{
	// virtuales y del kernel
	"tmpfs": {}, "devtmpfs": {}, "devfs": {}, "ramfs": {}, "proc": {}, "procfs": {}, "sysfs": {},
	"cgroup": {}, "cgroup2": {}, "debugfs": {}, "tracefs": {}, "securityfs": {}, "pstore": {},
	"bpf": {}, "configfs": {}, "fusectl": {}, "mqueue": {}, "hugetlbfs": {}, "autofs": {},
	"binfmt_misc": {}, "rpc_pipefs": {}, "nsfs": {}, "efivarfs": {}, "devpts": {}, "selinuxfs": {},
	// capas e imagenes (duplican el espacio de otro dispositivo o son de solo lectura)
	"overlay": {}, "overlayfs": {}, "aufs": {}, "squashfs": {}, "erofs": {}, "ecryptfs": {}, "fuse": {},
	// opticos
	"iso9660": {}, "udf": {}, "cdfs": {},
	// red
	"nfs": {}, "nfs4": {}, "cifs": {}, "smbfs": {}, "smb3": {}, "afs": {}, "ceph": {}, "glusterfs": {},
	"9p": {}, "vboxsf": {},
}

// skipMountPrefixes son montajes de sistema que no interesan: volumenes auxiliares
// de macOS (solo se conserva /System/Volumes/Data, ver isSkippedMount), y
// montajes internos de runtimes de contenedores.
var skipMountPrefixes = []string{
	"/private/var/vm", "/var/lib/docker/", "/var/lib/containers/", "/var/lib/kubelet/", "/run/", "/snap/",
}

const (
	macSystemVolumes = "/System/Volumes/"
	macDataVolume    = "/System/Volumes/Data"
)

func isSkippedMount(mount string) bool {
	if strings.HasPrefix(mount, macSystemVolumes) && mount != macDataVolume {
		return true
	}
	for _, p := range skipMountPrefixes {
		if mount == strings.TrimSuffix(p, "/") || strings.HasPrefix(mount, p) {
			return true
		}
	}
	return false
}

// selectMounts elige los montajes a medir: descarta pseudo-sistemas de archivos,
// montajes vacios o invalidos y lo que rechace keep (dependiente del SO); junta
// los duplicados por dispositivo (bind mounts, subvolumenes btrfs) quedandose con
// el punto de montaje mas corto; ordena de forma estable (mas corto primero, asi
// "/" o "C:" van al principio) y acota a maxDisks.
func selectMounts(parts []disk.PartitionStat, keep func(disk.PartitionStat) bool) []disk.PartitionStat {
	best := make(map[string]disk.PartitionStat, len(parts))
	for _, p := range parts {
		if !usableMount(p) || (keep != nil && !keep(p)) {
			continue
		}
		key := p.Device
		if key == "" {
			key = p.Mountpoint
		}
		if cur, ok := best[key]; !ok || mountLess(p.Mountpoint, cur.Mountpoint) {
			best[key] = p
		}
	}
	out := make([]disk.PartitionStat, 0, len(best))
	for _, p := range best {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b disk.PartitionStat) int {
		switch {
		case mountLess(a.Mountpoint, b.Mountpoint):
			return -1
		case mountLess(b.Mountpoint, a.Mountpoint):
			return 1
		}
		return 0
	})
	if len(out) > maxDisks {
		out = out[:maxDisks]
	}
	return out
}

func usableMount(p disk.PartitionStat) bool {
	m := p.Mountpoint
	if m == "" || len(m) > maxMountLen || !utf8.ValidString(m) || strings.ContainsRune(m, 0) {
		return false
	}
	fs := strings.ToLower(p.Fstype)
	if _, skip := skipFSTypes[fs]; skip || strings.HasPrefix(fs, "fuse.") {
		return false
	}
	return !isSkippedMount(m)
}

// mountLess ordena por largo y luego alfabeticamente: determinista.
func mountLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

// Disks mide el uso de cada montaje relevante. Un montaje que no se puede leer
// se omite (con aviso por cambio de estado) sin impedir los demas; falla solo si
// no se pudo listar ningun montaje.
func (s *System) Disks(ctx context.Context) ([]sender.DiskUsage, error) {
	parts, err := s.partitions(ctx)
	// gopsutil devuelve avisos junto con resultados validos (p. ej. una unidad de red desconectada).
	if err != nil && len(parts) == 0 {
		return nil, fmt.Errorf("listando particiones: %w", err)
	}
	var out []sender.DiskUsage
	for _, p := range selectMounts(parts, s.keepMount) {
		u, err := s.usage(ctx, p.Mountpoint)
		key := "disco " + p.Mountpoint
		if !s.optionalOK(ctx, key, err) {
			continue
		}
		if u == nil || mb(u.Total) == 0 {
			continue
		}
		out = append(out, sender.DiskUsage{Mount: p.Mountpoint, UsedMB: min(mb(u.Used), mb(u.Total)), TotalMB: mb(u.Total)})
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("midiendo discos: %w", ctx.Err())
	}
	return out, nil
}
