//go:build windows

package collector

import (
	"github.com/shirou/gopsutil/v4/disk"
	"golang.org/x/sys/windows"
)

// keepMount deja solo los discos fijos: excluye unidades opticas, de red y
// extraibles (un USB o una unidad de red desconectada no es almacenamiento del
// equipo, y consultarla puede colgar).
func keepMount(p disk.PartitionStat) bool {
	root, err := windows.UTF16PtrFromString(p.Mountpoint + `\`)
	if err != nil {
		return false
	}
	return windows.GetDriveType(root) == windows.DRIVE_FIXED
}
