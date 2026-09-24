package httpapi

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"argos/server/internal/store"
)

func metricsBody(extraMetrics, top string) string {
	m := `"cpu_pct":12.5,"mem_used_mb":100,"mem_total_mb":200,"net":{"rx_bytes":1,"tx_bytes":2}`
	if extraMetrics != "" {
		m += "," + extraMetrics
	}
	if top != "" {
		top = "," + top
	}
	return `{"timestamp":` + strconv.FormatInt(fixedNow.Unix(), 10) + `,"metrics":{` + m + `}` + top + `}`
}

func TestAgentMetricsNewFieldsValidation(t *testing.T) {
	t.Parallel()
	const token = "t"
	manyDisks := func(n int) string {
		parts := make([]string, n)
		for i := range n {
			parts[i] = fmt.Sprintf(`{"mount":"/m%d","used_mb":1,"total_mb":2}`, i)
		}
		return `"disks":[` + strings.Join(parts, ",") + `]`
	}
	manyIPs := func(n int) string {
		parts := make([]string, n)
		for i := range n {
			parts[i] = fmt.Sprintf(`"10.0.0.%d"`, i+1)
		}
		return `"host_info":{"ips":[` + strings.Join(parts, ",") + `]}`
	}
	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		// swap, load, bateria
		{"swap valido", metricsBody(`"swap":{"used_mb":0,"total_mb":2048}`, ""), 200},
		{"swap negativo", metricsBody(`"swap":{"used_mb":-1,"total_mb":2048}`, ""), 400},
		{"swap total negativo", metricsBody(`"swap":{"total_mb":-5}`, ""), 400},
		{"swap desborda int32", metricsBody(`"swap":{"used_mb":3000000000}`, ""), 400},
		{"load valido", metricsBody(`"load":{"l1":0.5,"l5":0.4,"l15":0.3}`, ""), 200},
		{"load parcial", metricsBody(`"load":{"l1":0.5}`, ""), 200},
		{"load negativo", metricsBody(`"load":{"l5":-0.1}`, ""), 400},
		{"load absurdo", metricsBody(`"load":{"l1":1e30}`, ""), 400},
		{"load null (Windows)", metricsBody(`"load":null`, ""), 200},
		{"bateria valida", metricsBody(`"battery":{"pct":87,"charging":true}`, ""), 200},
		{"bateria 0 y 100", metricsBody(`"battery":{"pct":0,"charging":false}`, ""), 200},
		{"bateria 101", metricsBody(`"battery":{"pct":101}`, ""), 400},
		{"bateria -1", metricsBody(`"battery":{"pct":-1}`, ""), 400},
		{"bateria pct decimal", metricsBody(`"battery":{"pct":50.5}`, ""), 400},
		// discos
		{"disco valido", metricsBody("", `"disks":[{"mount":"/","used_mb":50000,"total_mb":200000}]`), 200},
		{"disco lleno (used = total)", metricsBody("", `"disks":[{"mount":"C:","used_mb":10,"total_mb":10}]`), 200},
		{"disks vacio = ausente", metricsBody("", `"disks":[]`), 200},
		{"disks null", metricsBody("", `"disks":null`), 200},
		{"32 discos", metricsBody("", manyDisks(32)), 200},
		{"33 discos", metricsBody("", manyDisks(33)), 400},
		{"mount duplicado", metricsBody("", `"disks":[{"mount":"/","used_mb":1,"total_mb":2},{"mount":"/","used_mb":1,"total_mb":2}]`), 400},
		{"mount vacio", metricsBody("", `"disks":[{"mount":"","used_mb":1,"total_mb":2}]`), 400},
		{"mount 128", metricsBody("", `"disks":[{"mount":"`+strings.Repeat("a", 128)+`","used_mb":1,"total_mb":2}]`), 200},
		{"mount 129", metricsBody("", `"disks":[{"mount":"`+strings.Repeat("a", 129)+`","used_mb":1,"total_mb":2}]`), 400},
		{"mount con control", metricsBody("", `"disks":[{"mount":"a\u0007b","used_mb":1,"total_mb":2}]`), 400},
		{"mount con NUL", metricsBody("", `"disks":[{"mount":"a\u0000b","used_mb":1,"total_mb":2}]`), 400},
		{"mount con separador de linea", metricsBody("", `"disks":[{"mount":"a b","used_mb":1,"total_mb":2}]`), 400},
		{"total cero", metricsBody("", `"disks":[{"mount":"/","used_mb":0,"total_mb":0}]`), 400},
		{"used mayor que total", metricsBody("", `"disks":[{"mount":"/","used_mb":3,"total_mb":2}]`), 400},
		{"used negativo", metricsBody("", `"disks":[{"mount":"/","used_mb":-1,"total_mb":2}]`), 400},
		{"total desborda int32", metricsBody("", `"disks":[{"mount":"/","used_mb":1,"total_mb":3000000000}]`), 400},
		// host_info
		{"host_info completo", metricsBody("", `"host_info":{"boot_time":`+strconv.FormatInt(fixedNow.Add(-time.Hour).Unix(), 10)+`,"cpu_model":"Ryzen","cpu_cores":8,"cpu_threads":16,"os_name":"Ubuntu","os_version":"24.04","kernel_version":"6.8","agent_version":"0.4.1","ips":["192.168.1.25","fe80::1","::ffff:10.0.0.1"]}`), 200},
		{"host_info vacio", metricsBody("", `"host_info":{}`), 200},
		{"host_info null", metricsBody("", `"host_info":null`), 200},
		{"texto de 128", metricsBody("", `"host_info":{"cpu_model":"`+strings.Repeat("a", 128)+`"}`), 200},
		{"texto de 129", metricsBody("", `"host_info":{"cpu_model":"`+strings.Repeat("a", 129)+`"}`), 400},
		{"agent_version de 32", metricsBody("", `"host_info":{"agent_version":"`+strings.Repeat("1", 32)+`"}`), 200},
		{"agent_version de 33", metricsBody("", `"host_info":{"agent_version":"`+strings.Repeat("1", 33)+`"}`), 400},
		{"texto con NUL", metricsBody("", `"host_info":{"os_name":"a\u0000"}`), 400},
		{"16 ips", metricsBody("", manyIPs(16)), 200},
		{"17 ips", metricsBody("", manyIPs(17)), 400},
		{"ip no parseable", metricsBody("", `"host_info":{"ips":["999.1.1.1"]}`), 400},
		{"ip con zona", metricsBody("", `"host_info":{"ips":["fe80::1%eth0"]}`), 400},
		{"ip cidr", metricsBody("", `"host_info":{"ips":["10.0.0.0/8"]}`), 400},
		{"boot_time futuro", metricsBody("", `"host_info":{"boot_time":`+strconv.FormatInt(fixedNow.Add(time.Hour).Unix(), 10)+`}`), 400},
		{"boot_time absurdo pasado", metricsBody("", `"host_info":{"boot_time":1}`), 400},
		{"boot_time negativo", metricsBody("", `"host_info":{"boot_time":-5}`), 400},
		{"cpu_cores excesivo", metricsBody("", `"host_info":{"cpu_cores":5000}`), 400},
		{"cpu_threads excesivo", metricsBody("", `"host_info":{"cpu_threads":9000}`), 400},
		{"cpu_cores negativo", metricsBody("", `"host_info":{"cpu_cores":-1}`), 400},
		// agente viejo y campos desconocidos
		{"agente viejo", metricsBody("", `"services":[]`), 200},
		{"campo desconocido dentro de host_info", metricsBody("", `"host_info":{"nuevo":1}`), 200},
		{"campo desconocido en metrics", metricsBody(`"gpu":{"pct":1}`, ""), 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := &fakeStore{validHash: hashToken(token)}
			h, _ := newTestAPI(t, fs)
			rec, resp := do(t, h, "POST", "/api/v1/agent/metrics", token, tt.body)
			if rec.Code != tt.wantCode {
				t.Fatalf("status got %d, want %d (%s)", rec.Code, tt.wantCode, rec.Body.String())
			}
			if tt.wantCode == 400 {
				wantError(t, rec, resp, 400, "invalid_body")
				if len(fs.ingested) != 0 {
					t.Errorf("un reporte invalido no debe llegar al store")
				}
				if resp.Error.Message == "" {
					t.Errorf("el 400 debe explicar el motivo")
				}
			} else if len(fs.ingested) != 1 {
				t.Errorf("ingested got %d, want 1", len(fs.ingested))
			}
		})
	}
}

func TestAgentMetricsNewFieldsReachTheStore(t *testing.T) {
	t.Parallel()
	const token = "t"
	bt := fixedNow.Add(-2 * time.Hour).Unix()
	body := metricsBody(`"swap":{"used_mb":0,"total_mb":2048},"load":{"l1":0.5,"l5":0.4,"l15":0.3},"battery":{"pct":87,"charging":true}`,
		`"disks":[{"mount":"/","used_mb":50000,"total_mb":200000},{"mount":"/data","used_mb":1,"total_mb":2}],`+
			`"host_info":{"boot_time":`+strconv.FormatInt(bt, 10)+`,"cpu_model":"Ryzen","cpu_cores":8,"cpu_threads":16,"os_name":"Ubuntu","os_version":"24.04","kernel_version":"6.8","agent_version":"0.4.1","ips":["192.168.1.25","::ffff:10.0.0.1"]}`)
	fs := &fakeStore{validHash: hashToken(token)}
	h, _ := newTestAPI(t, fs)
	rec, _ := do(t, h, "POST", "/api/v1/agent/metrics", token, body)
	if rec.Code != 200 || len(fs.ingested) != 1 {
		t.Fatalf("status %d ingested %d (%s)", rec.Code, len(fs.ingested), rec.Body.String())
	}
	s := fs.ingested[0]
	// swap 0 es un cero real: puntero a 0, no nil.
	if s.SwapUsedMB == nil || *s.SwapUsedMB != 0 || s.SwapTotalMB == nil || *s.SwapTotalMB != 2048 {
		t.Errorf("swap got %v/%v", s.SwapUsedMB, s.SwapTotalMB)
	}
	if s.Load1 == nil || *s.Load1 != 0.5 || s.Load5 == nil || s.Load15 == nil || s.BatteryPct == nil || *s.BatteryPct != 87 || s.BatteryCharging == nil || !*s.BatteryCharging {
		t.Errorf("load/bateria got %+v", s)
	}
	if len(s.Disks) != 2 || s.Disks[0] != (store.DiskSample{Mount: "/", UsedMB: 50000, TotalMB: 200000}) {
		t.Errorf("disks got %+v", s.Disks)
	}
	i := s.Info
	if i == nil || i.CPUModel != "Ryzen" || i.CPUCores == nil || *i.CPUCores != 8 || i.CPUThreads == nil || *i.CPUThreads != 16 || i.AgentVersion != "0.4.1" ||
		i.BootTime == nil || i.BootTime.Unix() != bt || len(i.IPs) != 2 || i.IPs[1].String() != "10.0.0.1" {
		t.Errorf("info got %+v", i)
	}

	// Agente viejo: todo lo nuevo es nil / vacio (nunca cero).
	fs2 := &fakeStore{validHash: hashToken(token)}
	h2, _ := newTestAPI(t, fs2)
	do(t, h2, "POST", "/api/v1/agent/metrics", token, metricsBody("", ""))
	o := fs2.ingested[0]
	if o.SwapUsedMB != nil || o.SwapTotalMB != nil || o.Load1 != nil || o.Load5 != nil || o.Load15 != nil || o.BatteryPct != nil || o.BatteryCharging != nil || o.Disks != nil || o.Info != nil {
		t.Errorf("un agente viejo debe dejar todo lo nuevo en nil: %+v", o)
	}
}
