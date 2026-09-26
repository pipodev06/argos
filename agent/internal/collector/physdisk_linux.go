//go:build linux

package collector

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"

	"argos/agent/internal/sender"
)

func newSysfs() sysfs {
	return sysfs{root: "/sys", readlink: os.Readlink, devName: resolveDevName}
}

// resolveDevName devuelve el nombre del nodo de bloque (nvme0n1p2, dm-0) de una ruta
// /dev/...; sigue como maximo un enlace simbolico (/dev/mapper/x -> ../dm-0).
func resolveDevName(device string) string {
	if t, err := os.Readlink(device); err == nil {
		return filepath.Base(t)
	}
	// Servicio endurecido (PrivateDevices=true): /dev/mapper no existe y el enlace no se puede seguir; sysfs
	// (/sys/block/dm-N/dm/name) sigue disponible y da el nombre del nodo real (bug real: el / en LVM se perdia).
	if name, ok := strings.CutPrefix(device, "/dev/mapper/"); ok {
		if node := (sysfs{root: "/sys"}).dmNodeByName(name); node != "" {
			return node
		}
	}
	return filepath.Base(device)
}

func readPhysicalDisks(_ context.Context, parts []disk.PartitionStat) []sender.PhysicalDisk {
	return newSysfs().physicalDisks(parts)
}
