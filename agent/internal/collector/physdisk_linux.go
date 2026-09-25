//go:build linux

package collector

import (
	"context"
	"os"
	"path/filepath"

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
	return filepath.Base(device)
}

func readPhysicalDisks(_ context.Context, parts []disk.PartitionStat) []sender.PhysicalDisk {
	return newSysfs().physicalDisks(parts)
}
