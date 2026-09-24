package collector

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"argos/agent/internal/sender"
)

// psFixture crea un arbol tipo /sys/class/power_supply.
func psFixture(t *testing.T, supplies map[string]map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, files := range supplies {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		for f, content := range files {
			if err := os.WriteFile(filepath.Join(dir, f), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

// batteryCase es una fila de las tablas de parseo: exactamente uno de
// want, wantErr (errors.Is) o anyErr (error real distinto de ErrNoBattery).
type batteryCase struct {
	name    string
	want    sender.BatteryMetrics
	wantErr error
	anyErr  bool
}

func (c batteryCase) check(t *testing.T, got sender.BatteryMetrics, err error) {
	t.Helper()
	switch {
	case c.wantErr != nil:
		if !errors.Is(err, c.wantErr) {
			t.Fatalf("err got %v, want %v", err, c.wantErr)
		}
	case c.anyErr:
		if err == nil || errors.Is(err, ErrNoBattery) {
			t.Fatalf("err got %v, want un error real (no ErrNoBattery)", err)
		}
	default:
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got != c.want {
			t.Errorf("got %+v, want %+v", got, c.want)
		}
	}
}

func TestReadSysfsBatteries(t *testing.T) {
	t.Parallel()

	bat := func(capacity, status string) map[string]string {
		return map[string]string{"type": "Battery\n", "present": "1\n", "capacity": capacity + "\n", "status": status + "\n"}
	}
	tests := []struct {
		batteryCase
		supplies map[string]map[string]string
	}{
		{batteryCase{name: "BAT0 descargando", want: sender.BatteryMetrics{Pct: 87}},
			map[string]map[string]string{"BAT0": bat("87", "Discharging")}},
		{batteryCase{name: "BAT0 cargando", want: sender.BatteryMetrics{Pct: 54, Charging: true}},
			map[string]map[string]string{"BAT0": bat("54", "Charging")}},
		{batteryCase{name: "BAT0 llena y enchufada", want: sender.BatteryMetrics{Pct: 100, Charging: true}},
			map[string]map[string]string{"BAT0": bat("100", "Full")}},
		{batteryCase{name: "carga limitada por el fabricante", want: sender.BatteryMetrics{Pct: 80, Charging: true}},
			map[string]map[string]string{"BAT1": bat("80", "Not charging")}},
		{batteryCase{name: "dos baterias promedian y basta una enchufada", want: sender.BatteryMetrics{Pct: 75, Charging: true}},
			map[string]map[string]string{"BAT0": bat("50", "Discharging"), "BAT1": bat("100", "Charging")}},
		{batteryCase{name: "solo cargador AC: es un escritorio", wantErr: ErrNoBattery},
			map[string]map[string]string{"AC": {"type": "Mains\n", "online": "1\n"}}},
		{batteryCase{name: "sin power_supply", wantErr: ErrNoBattery},
			map[string]map[string]string{}},
		{batteryCase{name: "bateria ausente (present=0)", wantErr: ErrNoBattery},
			map[string]map[string]string{"BAT0": {"type": "Battery\n", "present": "0\n"}}},
		{batteryCase{name: "BAT* que no es Battery", wantErr: ErrNoBattery},
			map[string]map[string]string{"BAT0": {"type": "Mains\n"}}},
		{batteryCase{name: "estado Unknown no se adivina", anyErr: true},
			map[string]map[string]string{"BAT0": bat("99", "Unknown")}},
		{batteryCase{name: "capacity fuera de rango", anyErr: true},
			map[string]map[string]string{"BAT0": bat("150", "Full")}},
		{batteryCase{name: "capacity basura", anyErr: true},
			map[string]map[string]string{"BAT0": bat("abc", "Full")}},
		{batteryCase{name: "faltan archivos", anyErr: true},
			map[string]map[string]string{"BAT0": {"type": "Battery\n"}}},
		{batteryCase{name: "una buena y una rota: usa la buena", want: sender.BatteryMetrics{Pct: 60}},
			map[string]map[string]string{"BAT0": bat("60", "Discharging"), "BAT1": bat("zzz", "Discharging")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := readSysfsBatteries(psFixture(t, tt.supplies))
			tt.check(t, got, err)
		})
	}
}

func TestParsePmset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		batteryCase
		out string
	}{
		{batteryCase{name: "con bateria descargando", want: sender.BatteryMetrics{Pct: 87}},
			"Now drawing from 'Battery Power'\n -InternalBattery-0 (id=4653155)\t87%; discharging; 5:32 remaining present: true\n"},
		{batteryCase{name: "sin estimacion", want: sender.BatteryMetrics{Pct: 61}},
			"Now drawing from 'Battery Power'\n -InternalBattery-0 (id=4653155)\t61%; discharging; (no estimate) present: true\n"},
		{batteryCase{name: "cargando", want: sender.BatteryMetrics{Pct: 54, Charging: true}},
			"Now drawing from 'AC Power'\n -InternalBattery-0 (id=4653155)\t54%; charging; 1:12 remaining present: true\n"},
		{batteryCase{name: "cargada", want: sender.BatteryMetrics{Pct: 100, Charging: true}},
			"Now drawing from 'AC Power'\n -InternalBattery-0 (id=4653155)\t100%; charged; 0:00 remaining present: true\n"},
		{batteryCase{name: "terminando de cargar", want: sender.BatteryMetrics{Pct: 99, Charging: true}},
			"Now drawing from 'AC Power'\n -InternalBattery-0 (id=4653155)\t99%; finishing charge; 0:05 remaining present: true\n"},
		{batteryCase{name: "enchufada sin cargar (carga optimizada)", want: sender.BatteryMetrics{Pct: 80, Charging: true}},
			"Now drawing from 'AC Power'\n -InternalBattery-0 (id=4653155)\t80%; AC attached; not charging present: true\n"},
		{batteryCase{name: "estado nuevo desconocido: decide la fuente de energia", want: sender.BatteryMetrics{Pct: 70}},
			"Now drawing from 'Battery Power'\n -InternalBattery-0 (id=1)\t70%; en reposo; present: true\n"},
		{batteryCase{name: "escritorio sin bateria", wantErr: ErrNoBattery},
			"Now drawing from 'AC Power'\n"},
		{batteryCase{name: "salida vacia", wantErr: ErrNoBattery}, ""},
		{batteryCase{name: "bateria no presente", wantErr: ErrNoBattery},
			"Now drawing from 'AC Power'\n -InternalBattery-0 (id=1)\t0%; charging; 0:00 remaining present: false\n"},
		{batteryCase{name: "linea ilegible", anyErr: true},
			"Now drawing from 'AC Power'\n -InternalBattery-0 (id=1)\tsin porcentaje\n"},
		{batteryCase{name: "porcentaje absurdo", anyErr: true},
			"Now drawing from 'AC Power'\n -InternalBattery-0 (id=1)\t250%; charging\n"},
		{batteryCase{name: "estado raro y fuente desconocida", anyErr: true},
			" -InternalBattery-0 (id=1)\t70%; en reposo; present: true\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parsePmset(tt.out)
			tt.check(t, got, err)
		})
	}
}

func TestParsePowerStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		batteryCase
		st powerStatus
	}{
		{batteryCase{name: "portatil con bateria", want: sender.BatteryMetrics{Pct: 87}},
			powerStatus{ACLineStatus: 0, BatteryFlag: 1, BatteryLifePercent: 87}},
		{batteryCase{name: "bateria baja", want: sender.BatteryMetrics{Pct: 15}},
			powerStatus{ACLineStatus: 0, BatteryFlag: 2, BatteryLifePercent: 15}},
		{batteryCase{name: "critica", want: sender.BatteryMetrics{Pct: 4}},
			powerStatus{ACLineStatus: 0, BatteryFlag: 4, BatteryLifePercent: 4}},
		{batteryCase{name: "cargando", want: sender.BatteryMetrics{Pct: 54, Charging: true}},
			powerStatus{ACLineStatus: 1, BatteryFlag: 9, BatteryLifePercent: 54}},
		{batteryCase{name: "enchufada y llena (sin flag de carga)", want: sender.BatteryMetrics{Pct: 100, Charging: true}},
			powerStatus{ACLineStatus: 1, BatteryFlag: 1, BatteryLifePercent: 100}},
		{batteryCase{name: "flag desconocido pero con corriente", want: sender.BatteryMetrics{Pct: 70, Charging: true}},
			powerStatus{ACLineStatus: 1, BatteryFlag: 255, BatteryLifePercent: 70}},
		{batteryCase{name: "flag desconocido y sin corriente", want: sender.BatteryMetrics{Pct: 70}},
			powerStatus{ACLineStatus: 0, BatteryFlag: 255, BatteryLifePercent: 70}},
		{batteryCase{name: "escritorio: sin bateria", wantErr: ErrNoBattery},
			powerStatus{ACLineStatus: 1, BatteryFlag: 128, BatteryLifePercent: 255}},
		{batteryCase{name: "sin bateria con corriente desconocida", wantErr: ErrNoBattery},
			powerStatus{ACLineStatus: 255, BatteryFlag: 128, BatteryLifePercent: 255}},
		{batteryCase{name: "porcentaje desconocido", anyErr: true},
			powerStatus{ACLineStatus: 0, BatteryFlag: 1, BatteryLifePercent: 255}},
		{batteryCase{name: "todo desconocido", anyErr: true},
			powerStatus{ACLineStatus: 255, BatteryFlag: 255, BatteryLifePercent: 50}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parsePowerStatus(tt.st)
			tt.check(t, got, err)
		})
	}
}
