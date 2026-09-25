package collector

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"

	"argos/agent/internal/sender"
)

const (
	maxSysfsRead   = 256 // bytes leidos de un archivo de sysfs
	maxSlaveDepth  = 3   // profundidad al resolver dm/LVM por slaves/
	maxSlaveFanout = 16  // entradas de slaves/ que se recorren por nivel
)

// sysfs lee /sys con la raiz y las funciones del SO inyectables (tests).
type sysfs struct {
	root     string                       // "/sys"
	readlink func(string) (string, error) // os.Readlink
	devName  func(device string) string   // /dev/mapper/vg-root -> dm-0
}

func (s sysfs) physicalDisks(parts []disk.PartitionStat) []sender.PhysicalDisk {
	devOf := make(map[string]string, len(parts))
	mounts := make([]string, 0, len(parts))
	for _, p := range parts {
		if !strings.HasPrefix(p.Device, "/dev/") {
			continue
		}
		mounts = append(mounts, p.Mountpoint)
		devOf[p.Mountpoint] = s.devName(p.Device)
	}
	return groupPhysicalDisks(mounts,
		func(m string) (string, bool) { return s.parentDisk(devOf[m], 0) },
		s.meta)
}

// validBlockName rechaza nombres que escapen del directorio de sysfs o sean absurdos.
func validBlockName(n string) bool {
	return n != "" && n != "." && n != ".." && len(n) <= maxPhysIDLen && !strings.ContainsAny(n, "/\x00")
}

// parentDisk resuelve el disco fisico de un nodo de bloque: la particion sube a su
// disco padre; dm-* baja por slaves/ (profundidad y ramas acotadas) y solo se asocia
// si todo cae en un unico disco.
func (s sysfs) parentDisk(name string, depth int) (string, bool) {
	if !validBlockName(name) || depth > maxSlaveDepth {
		return "", false
	}
	if strings.HasPrefix(name, "dm-") || strings.HasPrefix(name, "md") {
		return s.throughSlaves(name, depth)
	}
	if _, err := os.Stat(filepath.Join(s.root, "class", "block", name, "partition")); err == nil {
		target, err := s.readlink(filepath.Join(s.root, "class", "block", name))
		if err != nil {
			return "", false
		}
		parent := filepath.Base(filepath.Dir(filepath.ToSlash(target)))
		if !validBlockName(parent) {
			return "", false
		}
		return parent, true
	}
	if _, err := os.Stat(filepath.Join(s.root, "block", name)); err != nil {
		return "", false
	}
	return name, true
}

func (s sysfs) throughSlaves(name string, depth int) (string, bool) {
	entries, err := os.ReadDir(filepath.Join(s.root, "block", name, "slaves"))
	if err != nil || len(entries) == 0 {
		return "", false
	}
	if len(entries) > maxSlaveFanout {
		entries = entries[:maxSlaveFanout]
	}
	found := ""
	for _, e := range entries {
		d, ok := s.parentDisk(e.Name(), depth+1)
		if !ok || (found != "" && found != d) {
			return "", false
		}
		found = d
	}
	return found, found != ""
}

func (s sysfs) readSmall(parts ...string) string {
	f, err := os.Open(filepath.Join(append([]string{s.root}, parts...)...))
	if err != nil {
		return ""
	}
	defer f.Close() //nolint:errcheck // solo lectura
	b, err := io.ReadAll(io.LimitReader(f, maxSysfsRead))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (s sysfs) meta(id string) diskMeta {
	var md diskMeta
	md.Model = s.readSmall("block", id, "device", "model")
	if sectors, err := strconv.ParseUint(s.readSmall("block", id, "size"), 10, 64); err == nil {
		if sectors <= maxPhysSizeMB<<11 { // evita desbordar al pasar a MiB (512 B/sector)
			md.SizeMB = int64(sectors >> 11) //nolint:gosec // acotado arriba
		}
	}
	if target, err := s.readlink(filepath.Join(s.root, "class", "block", id)); err == nil {
		md.Bus = busFromPath(id, filepath.ToSlash(target))
	}
	return md
}

// busFromPath deduce el bus del destino del enlace /sys/class/block/<disco>.
func busFromPath(id, target string) string {
	switch {
	case strings.Contains(target, "/nvme/") || strings.HasPrefix(id, "nvme"):
		return "nvme"
	case strings.Contains(target, "/usb"):
		return "usb"
	case strings.Contains(target, "/virtio"):
		return "virtual"
	case strings.Contains(target, "/ata"):
		return "sata"
	case strings.Contains(target, "/devices/virtual/"):
		return "virtual"
	case strings.Contains(target, "/target"):
		return "scsi"
	}
	return "other"
}
