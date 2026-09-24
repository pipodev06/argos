package store

import (
	"context"
	"slices"
	"testing"
)

func fptr(v float64) *float64 { return &v }

func TestListHostsMetricFilters(t *testing.T) {
	s := newTestStore(t)
	seedFleet(t, s)
	ctx := context.Background()

	// Bateria solo en alpha (15), Charlie (80) y bravo (5); el resto no tiene.
	for name, pct := range map[string]int{"alpha": 15, "Charlie": 80, "bravo": 5} {
		mustExec(t, `UPDATE host_latest SET battery_pct = $2 WHERE host_id = (SELECT id FROM hosts WHERE agent_token_hash = $1)`, "hash-"+name, pct)
	}

	// cpu: alpha 90, bravo 10, Charlie 40, foxtrot 5, dup 40 x2 (delta y echo sin dato).
	// mem %: alpha 50, bravo 6.25, Charlie 75, foxtrot 10, dup 25 x2. disco: alpha 50, bravo 97, foxtrot 10, dup 50 x2.
	// last_seen: alpha 5s, Charlie 10s, dup 20s/30s, bravo 1h, delta 2h, foxtrot 3h, echo NULL.
	tests := []struct {
		name   string
		filter HostFilter
		want   []string
	}{
		{"cpu_gt", HostFilter{CPUGt: fptr(39)}, []string{"alpha", "Charlie", "dup", "dup"}},
		{"cpu_gt es estricto", HostFilter{CPUGt: fptr(40)}, []string{"alpha"}},
		{"cpu_gt 0 excluye sin dato", HostFilter{CPUGt: fptr(0)}, []string{"alpha", "bravo", "Charlie", "dup", "dup", "foxtrot"}},
		{"mem_gt", HostFilter{MemGt: fptr(40)}, []string{"alpha", "Charlie"}},
		{"mem_gt sin total no cumple", HostFilter{MemGt: fptr(0)}, []string{"alpha", "bravo", "Charlie", "dup", "dup", "foxtrot"}},
		{"disk_gt", HostFilter{DiskGt: fptr(50)}, []string{"bravo"}},
		{"disk_gt sin disco no cumple", HostFilter{DiskGt: fptr(0)}, []string{"alpha", "bravo", "dup", "dup", "foxtrot"}},
		{"battery_lt", HostFilter{BatteryLt: fptr(20)}, []string{"alpha", "bravo"}},
		{"battery_lt 100 deja fuera sin bateria", HostFilter{BatteryLt: fptr(100)}, []string{"alpha", "bravo", "Charlie"}},
		{"seen_before", HostFilter{SeenBefore: 60}, []string{"bravo", "delta", "foxtrot"}},
		{"seen_before 2h30 (NULL no cumple)", HostFilter{SeenBefore: 9000}, []string{"foxtrot"}},
		{"combinado AND", HostFilter{CPUGt: fptr(30), DiskGt: fptr(40), Statuses: []string{"up"}}, []string{"alpha", "dup", "dup"}},
		{"combinado sin resultados", HostFilter{CPUGt: fptr(80), BatteryLt: fptr(10)}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.filter
			f.Sort, f.Limit = "name", 100
			page, err := s.ListHosts(ctx, f)
			if err != nil {
				t.Fatal(err)
			}
			if got := names(page); !slices.Equal(got, tt.want) && (len(got) != 0 || len(tt.want) != 0) {
				t.Errorf("hosts got %v, want %v", got, tt.want)
			}
			if page.Total != len(tt.want) {
				t.Errorf("total got %d, want %d", page.Total, len(tt.want))
			}
		})
	}
}

func TestListHostsMetricFiltersKeysetAndOSInfo(t *testing.T) {
	s := newTestStore(t)
	seedFleet(t, s)
	ctx := context.Background()

	mustExec(t, `INSERT INTO host_info (host_id, os_name, os_version) SELECT id, 'Ubuntu', '24.04' FROM hosts WHERE agent_token_hash = 'hash-alpha'`)

	// cpu > 30 = alpha, Charlie, dup, dup: paginado de a 1, el total y el cursor respetan el filtro.
	f := HostFilter{CPUGt: fptr(30), Sort: "cpu", Desc: true, Limit: 1}
	var got []string
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("paginacion sin fin")
		}
		page, err := s.ListHosts(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 4 {
			t.Fatalf("total got %d, want 4", page.Total)
		}
		for _, h := range page.Items {
			if h.Hostname == "alpha" {
				if h.OSName == nil || *h.OSName != "Ubuntu" || h.OSVersion == nil || *h.OSVersion != "24.04" {
					t.Errorf("os_name/os_version got %v/%v", h.OSName, h.OSVersion)
				}
			} else if h.OSName != nil || h.OSVersion != nil {
				t.Errorf("%s sin host_info debe traer nil, got %v/%v", h.Hostname, h.OSName, h.OSVersion)
			}
		}
		got = append(got, names(page)...)
		if page.NextCursor == nil {
			break
		}
		f.After = page.NextCursor
	}
	if want := []string{"alpha", "Charlie", "dup", "dup"}; !slices.Equal(got, want) {
		t.Errorf("paginas got %v, want %v", got, want)
	}
}
