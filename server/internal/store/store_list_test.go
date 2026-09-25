package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// seedFleet crea hosts con grupo, etiquetas y ultimo valor. Hostnames repetidos a proposito (el nombre no es unico).
func seedFleet(t *testing.T, s *Store) {
	t.Helper()
	servers := mkGroup(t, s, "Servidores", true, nil)
	laptops := mkGroup(t, s, "Laptops", false, nil)
	rows := []struct {
		name, status, os string
		group            string
		cpu              *float64
		memUsed, memTot  int
		disk             *float64
		ago              time.Duration
	}{
		{"alpha", "up", "linux", servers.ID, f64(90), 8000, 16000, f64(50), 5 * time.Second},
		{"bravo", "down", "linux", servers.ID, f64(10), 1000, 16000, f64(97), time.Hour},
		{"Charlie", "up", "windows", laptops.ID, f64(40), 12000, 16000, nil, 10 * time.Second},
		{"delta", "offline", "windows", laptops.ID, nil, 0, 0, nil, 2 * time.Hour},
		{"echo", "pending", "", "", nil, 0, 0, nil, 0},
		{"foxtrot", "disabled", "linux", "", f64(5), 100, 1000, f64(10), 3 * time.Hour},
		{"dup", "up", "darwin", "", f64(40), 4000, 16000, f64(50), 20 * time.Second},
		{"dup~", "up", "darwin", "", f64(40), 4000, 16000, f64(50), 30 * time.Second},
	}
	for _, r := range rows {
		var lastSeen *time.Duration
		if r.status != "pending" {
			lastSeen = ago(r.ago)
		}
		seedHost(t, r.name, r.status, lastSeen)
		mustExec(t, `UPDATE hosts SET os = NULLIF($2, '') WHERE agent_token_hash = $1`, "hash-"+r.name, r.os)
		if r.group != "" {
			mustExec(t, `UPDATE hosts SET group_id = $2 WHERE agent_token_hash = $1`, "hash-"+r.name, r.group)
		}
		if r.cpu != nil {
			var diskMount *string
			if r.disk != nil {
				m := "/"
				diskMount = &m
			}
			mustExec(t, `INSERT INTO host_latest (host_id, ts, cpu_pct, mem_used_mb, mem_total_mb, disk_max_pct, disk_max_mount)
			             SELECT id, now(), $2, $3, $4, $5, $6 FROM hosts WHERE agent_token_hash = $1`,
				"hash-"+r.name, r.cpu, r.memUsed, r.memTot, r.disk, diskMount)
		}
	}
	// El hostname no es unico (el token si): el segundo "dup" se sembro como "dup~".
	mustExec(t, `UPDATE hosts SET hostname = 'dup' WHERE hostname = 'dup~'`)
}

func f64(v float64) *float64 { return &v }

func names(p HostPage) []string {
	out := make([]string, len(p.Items))
	for i, h := range p.Items {
		out[i] = h.Hostname
	}
	return out
}

func TestListHostsFilters(t *testing.T) {
	s := newTestStore(t)
	seedFleet(t, s)
	ctx := context.Background()

	var groupID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM host_groups WHERE name = 'Laptops'`).Scan(&groupID); err != nil {
		t.Fatal(err)
	}

	var serversID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM host_groups WHERE name = 'Servidores'`).Scan(&serversID); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		filter HostFilter
		want   []string // por nombre, orden "name"
	}{
		{"todos", HostFilter{}, []string{"alpha", "bravo", "Charlie", "delta", "dup", "dup", "echo", "foxtrot"}},
		{"q subcadena sin mayusculas", HostFilter{Q: "HAR"}, []string{"Charlie"}},
		{"q sin resultados", HostFilter{Q: "zzz"}, nil},
		{"q no es LIKE: el % es literal", HostFilter{Q: "%"}, nil},
		{"un estado", HostFilter{Statuses: []string{"up"}}, []string{"alpha", "Charlie", "dup", "dup"}},
		{"varios estados", HostFilter{Statuses: []string{"down", "offline"}}, []string{"bravo", "delta"}},
		{"grupo", HostFilter{GroupIDs: []string{groupID}}, []string{"Charlie", "delta"}},
		{"sin grupo", HostFilter{NoGroup: true}, []string{"dup", "dup", "echo", "foxtrot"}},
		{"os", HostFilter{OSes: []string{"WINDOWS"}}, []string{"Charlie", "delta"}},
		{"varios grupos (OR)", HostFilter{GroupIDs: []string{groupID, serversID}}, []string{"alpha", "bravo", "Charlie", "delta"}},
		{"grupo o sin grupo", HostFilter{GroupIDs: []string{groupID}, NoGroup: true}, []string{"Charlie", "delta", "dup", "dup", "echo", "foxtrot"}},
		{"varios os (OR)", HostFilter{OSes: []string{"windows", "darwin"}}, []string{"Charlie", "delta", "dup", "dup"}},
		{"varios grupos y os (AND entre campos)", HostFilter{GroupIDs: []string{groupID, serversID}, OSes: []string{"linux", "darwin"}}, []string{"alpha", "bravo"}},
		{"grupo o none, os, estado y q", HostFilter{GroupIDs: []string{serversID}, NoGroup: true, OSes: []string{"linux", "darwin"},
			Statuses: []string{"up", "disabled"}, Q: "a"}, []string{"alpha"}},
		{"grupo o none con filtro numerico", HostFilter{GroupIDs: []string{groupID}, NoGroup: true, CPUGt: f64(30)}, []string{"Charlie", "dup", "dup"}},
		{"combinado", HostFilter{Statuses: []string{"up"}, NoGroup: true, OSes: []string{"darwin"}, Q: "du"}, []string{"dup", "dup"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := tt.filter
			f.Sort, f.Limit = "name", 100
			page, err := s.ListHosts(ctx, AllScope(), f)
			if err != nil {
				t.Fatal(err)
			}
			if got := names(page); !slices.Equal(got, tt.want) && (len(got) != 0 || len(tt.want) != 0) {
				t.Errorf("hosts got %v, want %v", got, tt.want)
			}
			if page.Total != len(tt.want) {
				t.Errorf("total got %d, want %d", page.Total, len(tt.want))
			}
			if page.Items == nil {
				t.Errorf("Items no debe ser nil")
			}
		})
	}
}

func TestListHostsSortsAndKeysetPagination(t *testing.T) {
	s := newTestStore(t)
	seedFleet(t, s)
	ctx := context.Background()

	wantOrder := map[string][]string{
		// severidad: down, pending, offline, up (por nombre), disabled
		"severity/asc": {"bravo", "echo", "delta", "alpha", "Charlie", "dup", "dup", "foxtrot"},
		"name/asc":     {"alpha", "bravo", "Charlie", "delta", "dup", "dup", "echo", "foxtrot"},
		"name/desc":    {"alpha", "bravo", "Charlie", "delta", "dup", "dup", "echo", "foxtrot"}, // desc solo invierte el valor: en un orden por nombre el valor ES el nombre
		"status/asc":   {"foxtrot", "bravo", "delta", "echo", "alpha", "Charlie", "dup", "dup"},
		// cpu: sin dato siempre al final (sin importar la direccion)
		"cpu/desc": {"alpha", "Charlie", "dup", "dup", "bravo", "foxtrot", "delta", "echo"},
		"cpu/asc":  {"foxtrot", "bravo", "Charlie", "dup", "dup", "alpha", "delta", "echo"},
		"mem/desc": {"Charlie", "alpha", "dup", "dup", "foxtrot", "bravo", "delta", "echo"},
		// disco: 97, 50, 50, 50, 10; sin dato al final
		"disk/desc": {"bravo", "alpha", "dup", "dup", "foxtrot", "Charlie", "delta", "echo"},
		// grupo: Laptops, Servidores, sin grupo al final
		"group/asc": {"Charlie", "delta", "alpha", "bravo", "dup", "dup", "echo", "foxtrot"},
		// last_seen desc: mas reciente primero; pending (NULL) al final
		"last_seen/desc": {"alpha", "Charlie", "dup", "dup", "bravo", "delta", "foxtrot", "echo"},
	}
	// name/desc: el valor de orden es lower(hostname) y ademas el tiebreak es siempre ascendente.
	wantOrder["name/desc"] = []string{"foxtrot", "echo", "dup", "dup", "delta", "Charlie", "bravo", "alpha"}

	for key, want := range wantOrder {
		sortName, dir, _ := strings.Cut(key, "/")
		for _, limit := range []int{100, 3, 1} {
			t.Run(fmt.Sprintf("%s/limit%d", key, limit), func(t *testing.T) {
				f := HostFilter{Sort: sortName, Desc: dir == "desc", Limit: limit}
				var got []string
				seen := map[string]bool{}
				for pages := 0; ; pages++ {
					if pages > 20 {
						t.Fatal("paginacion sin fin")
					}
					page, err := s.ListHosts(ctx, AllScope(), f)
					if err != nil {
						t.Fatal(err)
					}
					if page.Total != 8 {
						t.Fatalf("total got %d, want 8 (debe ser el de todo el filtro en cada pagina)", page.Total)
					}
					for _, h := range page.Items {
						if seen[h.ID] {
							t.Fatalf("host %s repetido entre paginas", h.ID)
						}
						seen[h.ID] = true
					}
					got = append(got, names(page)...)
					if page.NextCursor == nil {
						break
					}
					f.After = page.NextCursor
				}
				if !slices.Equal(got, want) {
					t.Errorf("orden got %v, want %v", got, want)
				}
			})
		}
	}
}

func TestListHostsRowContent(t *testing.T) {
	s := newTestStore(t)
	seedFleet(t, s)
	ctx := context.Background()
	mustExec(t, `INSERT INTO host_info (host_id, boot_time, ips) SELECT id, now() - interval '1 day', ARRAY['127.0.0.1', '192.168.1.25']::inet[]
	             FROM hosts WHERE hostname = 'alpha'`)
	mustExec(t, `UPDATE host_latest SET battery_pct = 80, battery_charging = true WHERE host_id = (SELECT id FROM hosts WHERE hostname = 'alpha')`)

	page, err := s.ListHosts(ctx, AllScope(), HostFilter{Sort: "name", Q: "alpha", Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("got (%+v, %v)", page, err)
	}
	h := page.Items[0]
	if h.Group == nil || h.Group.Name != "Servidores" {
		t.Errorf("grupo got %+v", h.Group)
	}
	if h.CPUPct == nil || *h.CPUPct != 90 || h.MemUsedMB == nil || *h.MemUsedMB != 8000 || h.DiskPct == nil || *h.DiskPct != 50 || h.DiskMount == nil || *h.DiskMount != "/" {
		t.Errorf("ultimo valor got %+v", h)
	}
	if len(h.IPs) != 2 || !h.IPs[0].IsLoopback() || h.BootTime == nil {
		t.Errorf("host_info got ips=%v boot=%v", h.IPs, h.BootTime)
	}
	if h.BatteryPct == nil || *h.BatteryPct != 80 || h.BatteryCharging == nil || !*h.BatteryCharging {
		t.Errorf("bateria got %v/%v", h.BatteryPct, h.BatteryCharging)
	}
	if h.LastSeenAt == nil || h.StatusChangedAt.IsZero() {
		t.Errorf("tiempos got %v / %v", h.LastSeenAt, h.StatusChangedAt)
	}
}

func TestSummary(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	empty, err := s.Summary(ctx, AllScope())
	if err != nil || empty.Counts.Total() != 0 || empty.TopCPU == nil || empty.RecentlyDown == nil || len(empty.ByGroup) != 0 {
		t.Fatalf("resumen vacio got (%+v, %v)", empty, err)
	}

	seedFleet(t, s)
	sum, err := s.Summary(ctx, AllScope())
	if err != nil {
		t.Fatal(err)
	}
	want := StatusCounts{Up: 4, Down: 1, Offline: 1, Pending: 1, Disabled: 1}
	if sum.Counts != want {
		t.Errorf("conteos got %+v, want %+v", sum.Counts, want)
	}
	// Grupos por nombre y "sin grupo" al final; solo grupos con hosts.
	if len(sum.ByGroup) != 3 || sum.ByGroup[0].Group.Name != "Laptops" || sum.ByGroup[1].Group.Name != "Servidores" || sum.ByGroup[2].Group != nil {
		t.Fatalf("by_group got %+v", sum.ByGroup)
	}
	if c := sum.ByGroup[0].Counts; c.Up != 1 || c.Offline != 1 || c.Total() != 2 {
		t.Errorf("Laptops got %+v", c)
	}
	if c := sum.ByGroup[2].Counts; c.Pending != 1 || c.Disabled != 1 || c.Up != 2 || c.Total() != 4 {
		t.Errorf("sin grupo got %+v", c)
	}
	// Rankings: solo up (bravo, down, y foxtrot, disabled, no entran aunque tengan cpu/disco).
	if len(sum.TopCPU) != 4 || sum.TopCPU[0].Hostname != "alpha" || sum.TopCPU[0].Value != 90 {
		t.Errorf("top_cpu got %+v", sum.TopCPU)
	}
	if len(sum.TopMem) != 4 || sum.TopMem[0].Hostname != "Charlie" || sum.TopMem[0].Value != 75 {
		t.Errorf("top_mem got %+v", sum.TopMem)
	}
	if len(sum.TopDisk) != 3 || sum.TopDisk[0].Value != 50 || sum.TopDisk[0].Mount == nil || *sum.TopDisk[0].Mount != "/" {
		t.Errorf("top_disk got %+v", sum.TopDisk)
	}
	// recently_down: solo down (no offline).
	if len(sum.RecentlyDown) != 1 || sum.RecentlyDown[0].Hostname != "bravo" || sum.RecentlyDown[0].Group == nil {
		t.Errorf("recently_down got %+v", sum.RecentlyDown)
	}

	// Acotado a 10.
	mustExec(t, `INSERT INTO hosts (hostname, agent_token_hash, status, last_seen_at) SELECT 'm' || g, 'hm' || g, 'down', now() FROM generate_series(1, 15) g`)
	sum, err = s.Summary(ctx, AllScope())
	if err != nil || len(sum.RecentlyDown) != topN {
		t.Errorf("recently_down acotado got %d (%v), want %d", len(sum.RecentlyDown), err, topN)
	}
}

func TestListHostsMultiValueFiltersKeysetPagination(t *testing.T) {
	s := newTestStore(t)
	seedFleet(t, s)
	ctx := context.Background()

	var groupID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM host_groups WHERE name = 'Laptops'`).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	// Laptops o sin grupo, solo windows/darwin: Charlie, delta, dup, dup.
	f := HostFilter{GroupIDs: []string{groupID}, NoGroup: true, OSes: []string{"windows", "darwin"}, Sort: "name", Limit: 2}
	var got []string
	for i := 0; i < 5; i++ {
		page, err := s.ListHosts(ctx, AllScope(), f)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 4 {
			t.Errorf("total got %d, want 4", page.Total)
		}
		got = append(got, names(page)...)
		if page.NextCursor == nil {
			break
		}
		f.After = page.NextCursor
	}
	if want := []string{"Charlie", "delta", "dup", "dup"}; !slices.Equal(got, want) {
		t.Errorf("paginado got %v, want %v", got, want)
	}
}
