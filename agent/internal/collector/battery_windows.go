//go:build windows

package collector

import (
	"context"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"

	"argos/agent/internal/sender"
)

var procGetSystemPowerStatus = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemPowerStatus")

// systemPowerStatus es SYSTEM_POWER_STATUS completo (la llamada escribe toda la estructura).
type systemPowerStatus struct {
	ACLineStatus        uint8
	BatteryFlag         uint8
	BatteryLifePercent  uint8
	SystemStatusFlag    uint8
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

func readBattery(context.Context) (sender.BatteryMetrics, error) {
	var st systemPowerStatus
	//nolint:gosec // llamada nativa: el puntero apunta a una estructura local con el layout de SYSTEM_POWER_STATUS
	r1, _, err := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&st)))
	if r1 == 0 {
		return sender.BatteryMetrics{}, fmt.Errorf("GetSystemPowerStatus: %w", err)
	}
	return parsePowerStatus(powerStatus{ACLineStatus: st.ACLineStatus, BatteryFlag: st.BatteryFlag, BatteryLifePercent: st.BatteryLifePercent})
}
