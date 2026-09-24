package collector

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"

	"argos/agent/internal/sender"
)

// TestSystemCollect usa el gopsutil real y solo verifica rangos sanos: los
// valores exactos dependen de la maquina. Si el entorno (sandbox de CI sin
// /proc, etc.) no permite medir, se salta en vez de fallar.
func TestSystemCollect(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s := NewSystem(nil)
	for i := range 2 { // la primera mide con ventana de 1 s; la segunda es delta inmediato
		m, err := s.Collect(ctx)
		if err != nil {
			t.Skipf("este entorno no permite medir el host (llamada %d): %v", i+1, err)
		}
		if m.CPUPct < 0 || m.CPUPct > 100 {
			t.Errorf("cpu_pct got %v, want en [0,100]", m.CPUPct)
		}
		if m.MemTotalMB <= 0 {
			t.Errorf("mem_total_mb got %d, want > 0", m.MemTotalMB)
		}
		if m.MemUsedMB < 0 || m.MemUsedMB > m.MemTotalMB {
			t.Errorf("mem_used_mb got %d, want en [0,%d]", m.MemUsedMB, m.MemTotalMB)
		}
		if m.Net.RxBytes < 0 || m.Net.TxBytes < 0 {
			t.Errorf("net got %+v, want contadores no negativos", m.Net)
		}
	}
}

// TestSystemRealOptionalReadings usa las lecturas reales de este host y solo
// exige coherencia: cada bloque opcional esta ausente o es valido (un CI sin
// bateria, sin swap o sin /sys no debe fallar).
func TestSystemRealOptionalReadings(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s := NewSystem(nil)
	m, err := s.Collect(ctx)
	if err != nil {
		t.Skipf("este entorno no permite medir el host: %v", err)
	}
	if m.Swap != nil && (m.Swap.TotalMB <= 0 || m.Swap.UsedMB < 0 || m.Swap.UsedMB > m.Swap.TotalMB) {
		t.Errorf("swap got %+v, want total>0 y 0<=used<=total (o ausente)", m.Swap)
	}
	if m.Load != nil && (m.Load.L1 < 0 || m.Load.L5 < 0 || m.Load.L15 < 0) {
		t.Errorf("load got %+v, want >= 0", m.Load)
	}
	if runtime.GOOS == "windows" && m.Load != nil {
		t.Errorf("load debe omitirse en Windows, got %+v", m.Load)
	}
	if m.Battery != nil && (m.Battery.Pct < 0 || m.Battery.Pct > 100) {
		t.Errorf("battery got %+v, want pct en [0,100]", m.Battery)
	}

	disks, err := s.Disks(ctx)
	if err != nil {
		t.Skipf("este entorno no permite listar discos: %v", err)
	}
	if len(disks) > maxDisks {
		t.Errorf("discos got %d, want <= %d", len(disks), maxDisks)
	}
	seen := map[string]bool{}
	for _, d := range disks {
		if d.Mount == "" || d.TotalMB <= 0 || d.UsedMB < 0 || d.UsedMB > d.TotalMB {
			t.Errorf("disco invalido: %+v", d)
		}
		if seen[d.Mount] {
			t.Errorf("montaje repetido: %s", d.Mount)
		}
		seen[d.Mount] = true
	}

	info, err := s.HostInfo(ctx)
	if err != nil {
		t.Skipf("este entorno no permite leer el host: %v", err)
	}
	if info.CPUThreads < 0 || info.CPUCores < 0 || len(info.IPs) > maxIPs {
		t.Errorf("host_info fuera de rango: %+v", info)
	}
}

func fakeSystem() *System {
	s := NewSystem(nil)
	s.swap = func(context.Context) (*mem.SwapMemoryStat, error) { return &mem.SwapMemoryStat{}, nil }
	s.load = nil
	s.battery = func(context.Context) (sender.BatteryMetrics, error) { return sender.BatteryMetrics{}, ErrNoBattery }
	return s
}

func TestOptionalBlocksAbsentWhenNoData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := fakeSystem()
	if got := s.readSwap(ctx); got != nil {
		t.Errorf("swap con total 0 debe omitirse, got %+v", got)
	}
	if got := s.readLoad(ctx); got != nil {
		t.Errorf("load sin soporte debe omitirse, got %+v", got)
	}
	if got := s.readBatteryBlock(ctx); got != nil {
		t.Errorf("sin bateria debe omitirse, got %+v", got)
	}
}

func TestOptionalBlocksPresentWhenData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := fakeSystem()
	s.swap = func(context.Context) (*mem.SwapMemoryStat, error) {
		return &mem.SwapMemoryStat{Total: 2048 << 20, Used: 0}, nil
	}
	s.load = func(context.Context) (*load.AvgStat, error) {
		return &load.AvgStat{Load1: 0.5, Load5: 0.4, Load15: 0.3}, nil
	}
	s.battery = func(context.Context) (sender.BatteryMetrics, error) {
		return sender.BatteryMetrics{Pct: 87, Charging: true}, nil
	}
	// swap con 0 usado es un dato real: se manda.
	if got := s.readSwap(ctx); got == nil || got.TotalMB != 2048 || got.UsedMB != 0 {
		t.Errorf("swap got %+v, want used 0 total 2048", got)
	}
	if got := s.readLoad(ctx); got == nil || got.L1 != 0.5 || got.L15 != 0.3 {
		t.Errorf("load got %+v", got)
	}
	if got := s.readBatteryBlock(ctx); got == nil || got.Pct != 87 || !got.Charging {
		t.Errorf("battery got %+v", got)
	}
}

func TestOptionalReadingFailureIsOmittedAndLoggedOnce(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	s := NewSystem(slog.New(slog.NewJSONHandler(&logs, nil)))
	s.load = nil
	s.swap = func(context.Context) (*mem.SwapMemoryStat, error) { return &mem.SwapMemoryStat{Total: 1 << 30}, nil }
	s.battery = func(context.Context) (sender.BatteryMetrics, error) {
		return sender.BatteryMetrics{}, errors.New("pmset roto")
	}
	ctx := context.Background()
	for range 4 {
		if got := s.readBatteryBlock(ctx); got != nil {
			t.Fatalf("una bateria ilegible debe omitirse, got %+v", got)
		}
		// Las demas lecturas siguen funcionando.
		if got := s.readSwap(ctx); got == nil {
			t.Fatal("un fallo de bateria no debe afectar al swap")
		}
	}
	if n := strings.Count(logs.String(), "pmset roto"); n != 1 {
		t.Errorf("el fallo se registro %d veces, want 1 (sin spam):\n%s", n, logs.String())
	}
}

func TestLoadWithInvalidValuesIsOmitted(t *testing.T) {
	t.Parallel()

	s := fakeSystem()
	s.load = func(context.Context) (*load.AvgStat, error) {
		return &load.AvgStat{Load1: math.NaN(), Load5: 1, Load15: 1}, nil
	}
	if got := s.readLoad(context.Background()); got != nil {
		t.Errorf("un NaN romperia el JSON del reporte: got %+v", got)
	}
}
