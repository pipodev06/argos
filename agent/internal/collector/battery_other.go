//go:build !linux && !darwin && !windows

package collector

import (
	"context"

	"argos/agent/internal/sender"
)

// readBattery: sin lectura de bateria en este SO (no se soporta), se omite el bloque.
func readBattery(context.Context) (sender.BatteryMetrics, error) {
	return sender.BatteryMetrics{}, ErrNoBattery
}
