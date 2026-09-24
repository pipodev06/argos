package collector

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"argos/agent/internal/sender"
)

// ErrNoBattery indica que el equipo no tiene bateria (escritorio, servidor, VM).
// No es un fallo: el bloque battery simplemente se omite.
var ErrNoBattery = errors.New("el equipo no tiene bateria")

// Convencion comun a todos los SO: Charging es true si el equipo esta conectado
// a corriente (cargando, llena o con la carga limitada por el fabricante) y false
// si funciona con bateria. Asi un portatil al 100 % enchufado no aparece como
// "descargando".

// --- Linux: /sys/class/power_supply/BAT* ---

// readSysfsBatteries recibe la raiz de power_supply (en Linux, /sys/class/power_supply)
// pa poder probarla con fixtures.

// parseSysfsBattery interpreta los archivos capacity y status de una bateria.
func parseSysfsBattery(capacity, status string) (sender.BatteryMetrics, error) {
	pct, err := strconv.Atoi(strings.TrimSpace(capacity))
	if err != nil || pct < 0 || pct > 100 {
		return sender.BatteryMetrics{}, fmt.Errorf("capacity invalida %q", strings.TrimSpace(capacity))
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "charging", "full", "not charging":
		return sender.BatteryMetrics{Pct: pct, Charging: true}, nil
	case "discharging":
		return sender.BatteryMetrics{Pct: pct, Charging: false}, nil
	default:
		// "Unknown" (algunos equipos lo reportan enchufados al 100 %): no se adivina.
		return sender.BatteryMetrics{}, fmt.Errorf("estado de bateria no reconocido %q", strings.TrimSpace(status))
	}
}

// readSysfsBatteries lee las baterias BAT* bajo root. Con varias baterias
// (portatiles con dos) el porcentaje es el promedio y Charging es true si alguna
// esta conectada.
func readSysfsBatteries(root string) (sender.BatteryMetrics, error) {
	dirs, err := filepath.Glob(filepath.Join(root, "BAT*"))
	if err != nil {
		return sender.BatteryMetrics{}, fmt.Errorf("buscando baterias: %w", err)
	}
	var all []sender.BatteryMetrics
	var firstErr error
	for _, dir := range dirs {
		if !isPresentBattery(dir) {
			continue
		}
		b, err := readSysfsBattery(dir)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", filepath.Base(dir), err)
			}
			continue
		}
		all = append(all, b)
	}
	switch {
	case len(all) > 0:
		return combineBatteries(all), nil
	case firstErr != nil:
		return sender.BatteryMetrics{}, firstErr
	default:
		return sender.BatteryMetrics{}, ErrNoBattery
	}
}

func readSysfsBattery(dir string) (sender.BatteryMetrics, error) {
	capacity, err := readSmallFile(filepath.Join(dir, "capacity"))
	if err != nil {
		return sender.BatteryMetrics{}, err
	}
	status, err := readSmallFile(filepath.Join(dir, "status"))
	if err != nil {
		return sender.BatteryMetrics{}, err
	}
	return parseSysfsBattery(capacity, status)
}

// isPresentBattery descarta entradas BAT* que no son una bateria fisica presente.
func isPresentBattery(dir string) bool {
	if t, err := readSmallFile(filepath.Join(dir, "type")); err == nil && !strings.EqualFold(strings.TrimSpace(t), "battery") {
		return false
	}
	if p, err := readSmallFile(filepath.Join(dir, "present")); err == nil && strings.TrimSpace(p) == "0" {
		return false
	}
	return true
}

func combineBatteries(bs []sender.BatteryMetrics) sender.BatteryMetrics {
	sum, charging := 0, false
	for _, b := range bs {
		sum += b.Pct
		charging = charging || b.Charging
	}
	return sender.BatteryMetrics{Pct: (sum + len(bs)/2) / len(bs), Charging: charging}
}

// readSmallFile lee un archivo de sysfs (unos pocos bytes).
func readSmallFile(path string) (string, error) {
	b, err := os.ReadFile(path) //nolint:gosec // ruta bajo /sys/class/power_supply o un directorio de prueba
	if err != nil {
		return "", fmt.Errorf("leyendo %s: %w", path, err)
	}
	return string(b), nil
}

// --- macOS: pmset -g batt ---

var pmsetBatteryLine = regexp.MustCompile(`(\d{1,3})%;\s*([^\n]*)`)

// parsePmset interpreta la salida de `pmset -g batt`, p. ej.:
//
//	Now drawing from 'Battery Power'
//	 -InternalBattery-0 (id=4653155)	87%; discharging; 5:32 remaining present: true
//
// Sin linea de bateria (o con "present: false") devuelve ErrNoBattery.
func parsePmset(out string) (sender.BatteryMetrics, error) {
	var onAC, knownSource bool
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Now drawing from") {
			onAC = strings.Contains(line, "'AC Power'")
			knownSource = onAC || strings.Contains(line, "'Battery Power'")
			continue
		}
		if !strings.Contains(line, "InternalBattery") {
			continue
		}
		if strings.Contains(line, "present: false") {
			return sender.BatteryMetrics{}, ErrNoBattery
		}
		m := pmsetBatteryLine.FindStringSubmatch(line)
		if m == nil {
			return sender.BatteryMetrics{}, fmt.Errorf("linea de bateria ilegible: %q", strings.TrimSpace(line))
		}
		pct, err := strconv.Atoi(m[1])
		if err != nil || pct > 100 {
			return sender.BatteryMetrics{}, fmt.Errorf("porcentaje invalido %q", m[1])
		}
		status := strings.ToLower(m[2])
		switch {
		case strings.Contains(status, "discharging"):
			return sender.BatteryMetrics{Pct: pct, Charging: false}, nil
		case strings.Contains(status, "charging"), strings.Contains(status, "charged"),
			strings.Contains(status, "finishing charge"), strings.Contains(status, "ac attached"):
			return sender.BatteryMetrics{Pct: pct, Charging: true}, nil
		case knownSource:
			return sender.BatteryMetrics{Pct: pct, Charging: onAC}, nil
		default:
			return sender.BatteryMetrics{}, fmt.Errorf("estado de bateria no reconocido: %q", strings.TrimSpace(m[2]))
		}
	}
	return sender.BatteryMetrics{}, ErrNoBattery
}

// --- Windows: GetSystemPowerStatus ---

// powerStatus son los campos de SYSTEM_POWER_STATUS que usamos.
// https://learn.microsoft.com/windows/win32/api/winbase/ns-winbase-system_power_status
type powerStatus struct {
	ACLineStatus       uint8 // 0 sin corriente, 1 con corriente, 255 desconocido
	BatteryFlag        uint8 // bits: 1 alta, 2 baja, 4 critica, 8 cargando, 128 sin bateria; 255 desconocido
	BatteryLifePercent uint8 // 0-100, 255 desconocido
}

const (
	powerUnknown       = 255
	batteryFlagCharge  = 8
	batteryFlagNoBatt  = 128
	acLineOnline       = 1
	maxBatteryLifePerc = 100
)

// parsePowerStatus interpreta el resultado de GetSystemPowerStatus.
func parsePowerStatus(st powerStatus) (sender.BatteryMetrics, error) {
	if st.BatteryFlag != powerUnknown && st.BatteryFlag&batteryFlagNoBatt != 0 {
		return sender.BatteryMetrics{}, ErrNoBattery
	}
	if st.BatteryLifePercent > maxBatteryLifePerc { // incluye 255 = desconocido
		return sender.BatteryMetrics{}, fmt.Errorf("porcentaje de bateria desconocido (%d)", st.BatteryLifePercent)
	}
	if st.BatteryFlag == powerUnknown && st.ACLineStatus == powerUnknown {
		return sender.BatteryMetrics{}, errors.New("estado de energia desconocido")
	}
	charging := st.ACLineStatus == acLineOnline || (st.BatteryFlag != powerUnknown && st.BatteryFlag&batteryFlagCharge != 0)
	return sender.BatteryMetrics{Pct: int(st.BatteryLifePercent), Charging: charging}, nil
}
