//go:build linux

package collector

import (
	"context"

	"argos/agent/internal/sender"
)

// sysfsPowerSupply es la ruta real de las fuentes de energia en Linux.
const sysfsPowerSupply = "/sys/class/power_supply"

func readBattery(context.Context) (sender.BatteryMetrics, error) {
	return readSysfsBatteries(sysfsPowerSupply)
}
