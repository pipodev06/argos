//go:build !linux && !windows

package collector

import (
	"context"

	"github.com/shirou/gopsutil/v4/disk"

	"argos/agent/internal/sender"
)

// readPhysicalDisks: macOS y otros SO no reportan discos fisicos (nil = desconocido).
func readPhysicalDisks(context.Context, []disk.PartitionStat) []sender.PhysicalDisk { return nil }
