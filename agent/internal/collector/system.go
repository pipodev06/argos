// Package collector mide las metricas reales del host con gopsutil y llamadas
// nativas del SO (bateria).
package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	psnet "github.com/shirou/gopsutil/v4/net"

	"argos/agent/internal/quiet"
	"argos/agent/internal/sender"
)

const (
	bytesPerMB = 1 << 20
	// maxMB acota los MiB que se reportan: el server valida enteros dentro de int32.
	maxMB = math.MaxInt32
	// firstCPUWindow es la ventana de la primera medicion de CPU: sin una lectura
	// previa no hay delta, y devolver 0 seria un dato falso.
	firstCPUWindow = time.Second
)

// System es el colector real. Mide de forma obligatoria CPU, memoria y red (si
// falla una, Collect falla y el ciclo se omite) y de forma opcional swap, load y
// bateria (si fallan, se omiten con un aviso por cambio de estado, sin spam).
// Tambien lee discos y datos del host (ver Disks y HostInfo).
// NO es seguro pa uso concurrente: un solo dueno (el loop del agente).
type System struct {
	quiet  *quiet.Tracker
	now    func() time.Time
	primed bool

	// Lecturas del SO, reemplazables en tests. load es nil donde no aplica (Windows).
	swap       func(context.Context) (*mem.SwapMemoryStat, error)
	load       func(context.Context) (*load.AvgStat, error)
	battery    func(context.Context) (sender.BatteryMetrics, error)
	partitions func(context.Context) ([]disk.PartitionStat, error)
	usage      func(context.Context, string) (*disk.UsageStat, error)
	keepMount  func(disk.PartitionStat) bool
	staticInfo func(context.Context) (sender.HostInfo, []error)
	interfaces func() ([]ifaceAddrs, error)
	physDisks  func(context.Context, []disk.PartitionStat) []sender.PhysicalDisk

	static     sender.HostInfo
	staticAt   time.Time
	staticDone bool

	phys physCache
}

// NewSystem crea el collector de este host. logger puede ser nil.
func NewSystem(logger *slog.Logger) *System {
	s := &System{
		quiet:   quiet.New(logger),
		now:     time.Now,
		swap:    mem.SwapMemoryWithContext,
		battery: readBattery,
		partitions: func(ctx context.Context) ([]disk.PartitionStat, error) {
			return disk.PartitionsWithContext(ctx, true)
		},
		usage:      disk.UsageWithContext,
		keepMount:  keepMount,
		staticInfo: readStaticInfo,
		interfaces: readInterfaces,
		physDisks:  readPhysicalDisks,
	}
	if runtime.GOOS != "windows" {
		// En Windows gopsutil emula el load con una goroutine de fondo: no es un load average real.
		s.load = load.AvgWithContext
	}
	return s
}

// Collect mide CPU global, memoria y contadores de red, y suma swap, load y
// bateria cuando el host los tiene. La primera llamada tarda ~1 s (ventana de
// medicion de CPU); las siguientes son inmediatas y miden el uso de CPU desde la
// llamada anterior.
func (s *System) Collect(ctx context.Context) (sender.Metrics, error) {
	pct, err := s.cpuPercent(ctx)
	if err != nil {
		return sender.Metrics{}, err
	}

	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return sender.Metrics{}, fmt.Errorf("midiendo memoria: %w", err)
	}

	rx, tx, err := netCounters(ctx)
	if err != nil {
		return sender.Metrics{}, err
	}

	m := sender.Metrics{
		CPUPct:     min(max(pct, 0), 100),
		MemUsedMB:  int(min(vm.Used/bytesPerMB, maxMB)),
		MemTotalMB: int(min(vm.Total/bytesPerMB, maxMB)),
		Net:        sender.NetMetrics{RxBytes: rx, TxBytes: tx},
	}
	m.Swap = s.readSwap(ctx)
	m.Load = s.readLoad(ctx)
	m.Battery = s.readBatteryBlock(ctx)
	return m, nil
}

// readSwap devuelve nil si no hay swap (en Windows total 0 = sin archivo de paginacion) o si falla.
func (s *System) readSwap(ctx context.Context) *sender.SwapMetrics {
	st, err := s.swap(ctx)
	if !s.optionalOK(ctx, "swap", err) {
		return nil
	}
	if st == nil || st.Total == 0 {
		return nil
	}
	return &sender.SwapMetrics{
		UsedMB:  mb(min(st.Used, st.Total)),
		TotalMB: mb(st.Total),
	}
}

// readLoad devuelve nil donde no hay load average (Windows) o si la lectura falla o no es finita.
func (s *System) readLoad(ctx context.Context) *sender.LoadMetrics {
	if s.load == nil {
		return nil
	}
	st, err := s.load(ctx)
	if !s.optionalOK(ctx, "load", err) {
		return nil
	}
	if st == nil || !finiteNonNegative(st.Load1, st.Load5, st.Load15) {
		s.optionalOK(ctx, "load", errors.New("load average invalido"))
		return nil
	}
	return &sender.LoadMetrics{L1: st.Load1, L5: st.Load5, L15: st.Load15}
}

// readBatteryBlock devuelve nil en equipos sin bateria (no es un fallo) o si la lectura falla.
func (s *System) readBatteryBlock(ctx context.Context) *sender.BatteryMetrics {
	b, err := s.battery(ctx)
	if errors.Is(err, ErrNoBattery) {
		return nil
	}
	if !s.optionalOK(ctx, "bateria", err) {
		return nil
	}
	return &b
}

// optionalOK lleva el registro de una lectura opcional y dice si su resultado sirve.
// Un contexto cancelado (apagado) no cuenta como fallo de la lectura.
func (s *System) optionalOK(ctx context.Context, key string, err error) bool {
	switch {
	case err == nil:
		s.quiet.OK(ctx, key)
		return true
	case ctx.Err() != nil:
		return false
	default:
		s.quiet.Fail(ctx, key, err)
		return false
	}
}

func finiteNonNegative(vals ...float64) bool {
	for _, v := range vals {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return false
		}
	}
	return true
}

// mb pasa bytes a MiB enteros, acotado a lo que el server acepta.
func mb(bytes uint64) int64 { return int64(min(bytes/bytesPerMB, maxMB)) }

// netCounters suma los contadores acumulados de todas las interfaces no-loopback.
func netCounters(ctx context.Context) (rx, tx int64, err error) {
	ifaces, err := psnet.InterfacesWithContext(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("listando interfaces de red: %w", err)
	}
	loopback := make(map[string]bool, len(ifaces))
	for _, i := range ifaces {
		for _, f := range i.Flags {
			if f == "loopback" {
				loopback[i.Name] = true
			}
		}
	}
	counters, err := psnet.IOCountersWithContext(ctx, true)
	if err != nil {
		return 0, 0, fmt.Errorf("leyendo contadores de red: %w", err)
	}
	var rxU, txU uint64
	for _, c := range counters {
		if loopback[c.Name] {
			continue
		}
		rxU += c.BytesRecv
		txU += c.BytesSent
	}
	return int64(min(rxU, math.MaxInt64)), int64(min(txU, math.MaxInt64)), nil //nolint:gosec // acotado a MaxInt64 arriba
}

// cpuPercent devuelve el uso global de CPU (0-100). gopsutil con ventana 0 mide
// el delta desde la ultima llamada con ventana 0 (estado global del paquete).
func (s *System) cpuPercent(ctx context.Context) (float64, error) {
	window := time.Duration(0)
	if !s.primed {
		window = firstCPUWindow
	}
	pcts, err := cpu.PercentWithContext(ctx, window, false)
	if err != nil {
		return 0, fmt.Errorf("midiendo cpu: %w", err)
	}
	if !s.primed {
		// Una ventana explicita no actualiza la base de gopsutil: se fija ahora pa
		// que la proxima lectura mida desde aqui y no desde el arranque del proceso.
		if _, err := cpu.PercentWithContext(ctx, 0, false); err != nil {
			return 0, fmt.Errorf("fijando base de cpu: %w", err)
		}
		s.primed = true
	}
	if len(pcts) != 1 || math.IsNaN(pcts[0]) || math.IsInf(pcts[0], 0) {
		return 0, errors.New("midiendo cpu: lectura invalida")
	}
	return pcts[0], nil
}
