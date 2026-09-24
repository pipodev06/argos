package sender

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestReportOmitsAbsentBlocks(t *testing.T) {
	t.Parallel()

	b, err := json.Marshal(report(1))
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"disks", "host_info", "services"} {
		if _, ok := top[k]; ok {
			t.Errorf("%q deberia estar ausente sin datos: %s", k, b)
		}
	}
	var metrics map[string]json.RawMessage
	if err := json.Unmarshal(top["metrics"], &metrics); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"swap", "load", "battery"} {
		if _, ok := metrics[k]; ok {
			t.Errorf("metrics.%s deberia estar ausente sin dato (no un cero inventado): %s", k, b)
		}
	}
	for _, k := range []string{"cpu_pct", "mem_used_mb", "mem_total_mb", "net"} {
		if _, ok := metrics[k]; !ok {
			t.Errorf("metrics.%s es obligatorio: %s", k, b)
		}
	}
}

func TestReportPresentBlocksMatchContract(t *testing.T) {
	t.Parallel()

	r := Report{
		Hostname: "h", AgentVersion: "0.4.1", Timestamp: 1758600000,
		Metrics: Metrics{
			CPUPct: 12.5, MemUsedMB: 4096, MemTotalMB: 16000,
			Net:     NetMetrics{RxBytes: 1, TxBytes: 2},
			Swap:    &SwapMetrics{UsedMB: 0, TotalMB: 2048},
			Load:    &LoadMetrics{L1: 0.5, L5: 0.4, L15: 0.3},
			Battery: &BatteryMetrics{Pct: 87, Charging: true},
		},
		Disks: []DiskUsage{{Mount: "/", UsedMB: 50000, TotalMB: 200000}},
		HostInfo: &HostInfo{
			BootTime: 1758400000, CPUModel: "cpu", CPUCores: 8, CPUThreads: 16,
			OSName: "os", OSVersion: "1", KernelVersion: "k", AgentVersion: "0.4.1", IPs: []string{"192.168.1.25"},
		},
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	// used_mb: 0 es un dato real (swap sin usar) y debe viajar.
	for _, want := range []string{
		`"swap":{"used_mb":0,"total_mb":2048}`,
		`"load":{"l1":0.5,"l5":0.4,"l15":0.3}`,
		`"battery":{"pct":87,"charging":true}`,
		`"disks":[{"mount":"/","used_mb":50000,"total_mb":200000}]`,
		`"host_info":{"boot_time":1758400000,"cpu_model":"cpu","cpu_cores":8,"cpu_threads":16,"os_name":"os","os_version":"1","kernel_version":"k","agent_version":"0.4.1","ips":["192.168.1.25"]}`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("falta %s en %s", want, b)
		}
	}
}

func TestBatteryNotChargingIsSentAsFalse(t *testing.T) {
	t.Parallel()

	b, err := json.Marshal(Metrics{Battery: &BatteryMetrics{Pct: 40, Charging: false}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"battery":{"pct":40,"charging":false}`) {
		t.Errorf("charging=false es un dato y debe viajar: %s", b)
	}
}

func TestHostInfoEqual(t *testing.T) {
	t.Parallel()

	base := HostInfo{BootTime: 1000, CPUModel: "m", CPUCores: 4, CPUThreads: 8, OSName: "o", OSVersion: "1", KernelVersion: "k", AgentVersion: "v", IPs: []string{"10.0.0.1", "10.0.0.2"}}
	tests := []struct {
		name   string
		mutate func(*HostInfo)
		want   bool
	}{
		{name: "igual", mutate: func(*HostInfo) {}, want: true},
		{name: "boot_time con jitter de segundos", mutate: func(h *HostInfo) { h.BootTime += 3 }, want: true},
		{name: "boot_time distinto (reinicio)", mutate: func(h *HostInfo) { h.BootTime += 3600 }, want: false},
		{name: "ip nueva", mutate: func(h *HostInfo) { h.IPs = []string{"10.0.0.1", "10.0.0.9"} }, want: false},
		{name: "menos ips", mutate: func(h *HostInfo) { h.IPs = []string{"10.0.0.1"} }, want: false},
		{name: "version del kernel", mutate: func(h *HostInfo) { h.KernelVersion = "k2" }, want: false},
		{name: "version del agente", mutate: func(h *HostInfo) { h.AgentVersion = "v2" }, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			other := base
			other.IPs = append([]string(nil), base.IPs...)
			tt.mutate(&other)
			if got := base.Equal(other); got != tt.want {
				t.Errorf("Equal got %v, want %v", got, tt.want)
			}
		})
	}
}

// Un desborde del buffer no debe perder discos/host_info (se envian solo de vez
// en cuando): el reporte que se descarta los deja al siguiente, sin duplicarlos.
func TestBufferOverflowCarriesOccasionalDataForwardWithoutDuplicating(t *testing.T) {
	t.Parallel()

	var got []Report
	var down atomic.Bool
	down.Store(true)
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if down.Load() {
			return nil, errors.New("sin red")
		}
		var rep Report
		if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
			return nil, err
		}
		got = append(got, rep)
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: http.Header{}}, nil
	})
	c, _ := newClient(t, "http://argos.invalid", func(o *Options) {
		o.MaxBuffer = 2
		o.HTTPClient = &http.Client{Transport: rt}
	})

	first := report(1)
	first.Disks = []DiskUsage{{Mount: "/", UsedMB: 1, TotalMB: 2}}
	first.HostInfo = &HostInfo{OSName: "viejo"}
	third := report(3)
	third.HostInfo = &HostInfo{OSName: "nuevo"} // mas reciente: gana sobre el descartado
	for _, r := range []Report{first, report(2), third, report(4)} {
		_ = c.Send(context.Background(), r) // falla a proposito: red caida
	}
	down.Store(false)
	if err := c.Send(context.Background(), report(5)); err != nil {
		t.Fatal(err)
	}

	// Con tope 2 se descartaron 1, 2 y 3; se entregan 4 y 5 y todo lo ocasional viaja en uno solo.
	var disks, hostInfos int
	for _, r := range got {
		if r.Disks != nil {
			disks++
		}
		if r.HostInfo != nil {
			hostInfos++
			if r.HostInfo.OSName != "nuevo" {
				t.Errorf("host_info got %q, want el mas reciente", r.HostInfo.OSName)
			}
		}
	}
	if disks != 1 {
		t.Errorf("reportes con disks got %d, want exactamente 1 (heredado, sin duplicar): %+v", disks, got)
	}
	if hostInfos != 1 {
		t.Errorf("reportes con host_info got %d, want exactamente 1: %+v", hostInfos, got)
	}
}
