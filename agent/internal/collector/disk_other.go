//go:build !windows

package collector

import "github.com/shirou/gopsutil/v4/disk"

// keepMount: fuera de Windows el filtrado por tipo de sistema de archivos basta.
func keepMount(disk.PartitionStat) bool { return true }
