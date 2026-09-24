//go:build darwin

package collector

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"argos/agent/internal/sender"
)

const (
	pmsetPath    = "/usr/bin/pmset" // ruta absoluta: un servicio no debe resolver ejecutables por PATH
	pmsetTimeout = 5 * time.Second
)

func readBattery(ctx context.Context) (sender.BatteryMetrics, error) {
	ctx, cancel := context.WithTimeout(ctx, pmsetTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, pmsetPath, "-g", "batt").Output()
	if err != nil {
		return sender.BatteryMetrics{}, fmt.Errorf("ejecutando pmset: %w", err)
	}
	return parsePmset(string(out))
}
