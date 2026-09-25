package collector

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/disk"

	"argos/agent/internal/sender"
)

func TestGroupPhysicalDisksTwoDisksThreeVolumes(t *testing.T) {
	t.Parallel()
	diskOf := map[string]string{"C:": "0", "D:": "1", "E:": "1"}
	metas := map[string]diskMeta{
		"0": {Model: "WDC PC SN530", Bus: "nvme", SizeMB: 488386},
		"1": {Model: "SAMSUNG MZALQ512HBLU-00BL1", Bus: "nvme", SizeMB: 488386},
	}
	got := groupPhysicalDisks([]string{"C:", "D:", "E:", "Z:"},
		func(m string) (string, bool) { id, ok := diskOf[m]; return id, ok },
		func(id string) diskMeta { return metas[id] })
	want := []sender.PhysicalDisk{
		{ID: "0", Model: "WDC PC SN530", Bus: "nvme", SizeMB: 488386, Mounts: []string{"C:"}},
		{ID: "1", Model: "SAMSUNG MZALQ512HBLU-00BL1", Bus: "nvme", SizeMB: 488386, Mounts: []string{"D:", "E:"}},
	}
	if !reflect.DeepEqual(got, want) { // Z: sin disco: no aparece
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestGroupPhysicalDisksMissingAndInvalidFields(t *testing.T) {
	t.Parallel()
	got := groupPhysicalDisks([]string{"/"},
		func(string) (string, bool) { return "sda", true },
		func(string) diskMeta { return diskMeta{Model: "  \x00\x01 ", Bus: "firewire", SizeMB: -5} })
	want := []sender.PhysicalDisk{{ID: "sda", Mounts: []string{"/"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if got := groupPhysicalDisks([]string{"/"}, func(string) (string, bool) { return "", false }, nil); len(got) != 0 {
		t.Fatalf("sin asociacion: got %+v, want vacio", got)
	}
}

func TestGroupPhysicalDisksLimits(t *testing.T) {
	t.Parallel()
	var mounts []string
	for i := range 40 {
		mounts = append(mounts, "/m"+strconv.Itoa(i))
	}
	mounts = append(mounts, "/"+strings.Repeat("x", 80)) // mas de 64: fuera
	one := groupPhysicalDisks(mounts, func(string) (string, bool) { return "d", true }, func(string) diskMeta { return diskMeta{SizeMB: maxPhysSizeMB + 1} })
	if len(one) != 1 || len(one[0].Mounts) != maxPhysMounts || one[0].SizeMB != 0 {
		t.Fatalf("got %+v, want 1 disco con %d mounts y size 0", one, maxPhysMounts)
	}

	var many []string
	for i := range 20 {
		many = append(many, "/v"+strconv.Itoa(i))
	}
	byMount := func(m string) (string, bool) { return strings.TrimPrefix(m, "/v"), true }
	got := groupPhysicalDisks(many, byMount, func(string) diskMeta { return diskMeta{} })
	if len(got) != maxPhysDisks {
		t.Fatalf("discos = %d, want %d", len(got), maxPhysDisks)
	}
	if got[2].ID != "2" || got[10].ID != "10" { // orden numerico: 2 antes que 10
		t.Fatalf("orden: got %q,%q", got[2].ID, got[10].ID)
	}
	long := func(string) (string, bool) { return strings.Repeat("i", 33), true }
	if g := groupPhysicalDisks([]string{"/a"}, long, nil); len(g) != 0 {
		t.Fatalf("id largo debe descartarse, got %+v", g)
	}
}

func TestCleanModel(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, in, want string }{
		{"espacios", "  WDC   PC\tSN530 \n", "WDC PC SN530"},
		{"control", "A\x00B\x7fC", "A B C"},
		{"utf8 invalido", "ab\xffcd", "abcd"},
		{"trunca sin partir runa", strings.Repeat("é", 50), strings.Repeat("é", 32)},
	}
	for _, tc := range tests {
		if got := cleanModel(tc.in); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSystemPhysicalDisksCachesAndDetectsMountChange(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	s := NewSystem(nil)
	s.now = func() time.Time { return now }
	s.keepMount = nil
	parts := []disk.PartitionStat{{Device: "C:", Mountpoint: "C:", Fstype: "NTFS"}}
	s.partitions = func(context.Context) ([]disk.PartitionStat, error) { return parts, nil }
	calls := 0
	s.physDisks = func(_ context.Context, p []disk.PartitionStat) []sender.PhysicalDisk {
		calls++
		return []sender.PhysicalDisk{{ID: "0", Mounts: []string{p[0].Mountpoint}}}
	}
	ctx := context.Background()
	s.physicalDisks(ctx)
	s.physicalDisks(ctx)
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (cache)", calls)
	}
	parts = append(parts, disk.PartitionStat{Device: "D:", Mountpoint: "D:", Fstype: "NTFS"})
	s.physicalDisks(ctx)
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (cambio de montajes)", calls)
	}
	now = now.Add(staticInfoTTL)
	s.physicalDisks(ctx)
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (expira la hora)", calls)
	}
}

// --- sysfs con arbol de fixtures (raiz y readlink inyectados: corre en cualquier SO) ---

type fixture struct {
	root  string
	links map[string]string
}

func (f *fixture) write(t *testing.T, rel, content string) {
	t.Helper()
	p := filepath.Join(f.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) sysfs() sysfs {
	return sysfs{
		root: f.root,
		readlink: func(p string) (string, error) {
			if t, ok := f.links[filepath.ToSlash(p)]; ok {
				return t, nil
			}
			return "", os.ErrNotExist
		},
		devName: func(d string) string {
			if d == "/dev/mapper/vg-root" {
				return "dm-0"
			}
			return filepath.Base(d)
		},
	}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{root: t.TempDir(), links: map[string]string{}}
	link := func(name, target string) {
		f.links[filepath.ToSlash(filepath.Join(f.root, "class", "block", name))] = target
	}
	const nvme = "../../devices/pci0000:00/0000:00:1d.0/0000:3d:00.0/nvme/nvme0/nvme0n1"
	const usb = "../../devices/pci0000:00/0000:00:14.0/usb2/2-1/2-1:1.0/host6/target6:0:0/6:0:0:0/block/sda"
	// nvme0n1 (2 particiones, p2 es PV de LVM) y sda USB.
	f.write(t, "block/nvme0n1/size", "1000215216\n")
	f.write(t, "block/nvme0n1/device/model", "SAMSUNG MZALQ512HBLU-00BL1   \n")
	link("nvme0n1", nvme)
	f.write(t, "class/block/nvme0n1p1/partition", "1\n")
	link("nvme0n1p1", nvme+"/nvme0n1p1")
	f.write(t, "class/block/nvme0n1p2/partition", "2\n")
	link("nvme0n1p2", nvme+"/nvme0n1p2")
	f.write(t, "block/dm-0/slaves/nvme0n1p2/.keep", "")
	f.write(t, "block/sda/size", "62914560\n")
	f.write(t, "block/sda/device/model", "Ultra USB 3.0\n")
	link("sda", usb)
	f.write(t, "class/block/sda1/partition", "1\n")
	link("sda1", usb+"/sda1")
	return f
}

func TestSysfsPhysicalDisks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	parts := []disk.PartitionStat{
		{Device: "/dev/nvme0n1p1", Mountpoint: "/boot"},
		{Device: "/dev/mapper/vg-root", Mountpoint: "/"},
		{Device: "/dev/sda1", Mountpoint: "/mnt/usb"},
		{Device: "/dev/nvme0n1p9", Mountpoint: "/huerfano"}, // sin datos en sysfs
		{Device: "server:/x", Mountpoint: "/red"},           // no es /dev
	}
	got := f.sysfs().physicalDisks(parts)
	want := []sender.PhysicalDisk{
		{ID: "sda", Model: "Ultra USB 3.0", Bus: "usb", SizeMB: 30720, Mounts: []string{"/mnt/usb"}},
		{ID: "nvme0n1", Model: "SAMSUNG MZALQ512HBLU-00BL1", Bus: "nvme", SizeMB: 488386, Mounts: []string{"/boot", "/"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestSysfsHostileShapes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write(t, "block/dm-1/slaves/dm-2/.keep", "") // dm circulares
	f.write(t, "block/dm-2/slaves/dm-1/.keep", "")
	f.write(t, "block/dm-3/slaves/nvme0n1p1/.keep", "") // dos discos distintos: no se asocia
	f.write(t, "block/dm-3/slaves/sda1/.keep", "")
	f.write(t, "block/dm-4/size", "1") // dm sin slaves
	s := f.sysfs()
	for _, name := range []string{"dm-1", "dm-3", "dm-4", "dm-99", "..", strings.Repeat("a", 300), "a/b"} {
		if id, ok := s.parentDisk(name, 0); ok {
			t.Errorf("parentDisk(%q) = %q, want sin asociar", name, id)
		}
	}
	f.write(t, "block/nvme0n1/device/model", strings.Repeat("M", 100000)) // se lee con tope
	if m := s.meta("nvme0n1").Model; len(m) != maxSysfsRead {
		t.Errorf("len(model) = %d, want %d (tope de lectura)", len(m), maxSysfsRead)
	}
	f.write(t, "block/nvme0n1/size", "18446744073709551615") // tamano absurdo: se omite
	if sz := s.meta("nvme0n1").SizeMB; sz != 0 {
		t.Errorf("size = %d, want 0", sz)
	}
}

func TestBusFromPath(t *testing.T) {
	t.Parallel()
	tests := []struct{ id, target, want string }{
		{"nvme0n1", "../../devices/pci/nvme/nvme0/nvme0n1", "nvme"},
		{"sda", "../../devices/pci/usb2/2-1/host6/target6:0:0/block/sda", "usb"},
		{"vda", "../../devices/pci/virtio2/block/vda", "virtual"},
		{"sda", "../../devices/pci/ata1/host0/target0:0:0/block/sda", "sata"},
		{"sdb", "../../devices/pci/host2/target2:0:0/block/sdb", "scsi"},
		{"loop0", "../../devices/virtual/block/loop0", "virtual"},
		{"x", "../../devices/raro", "other"},
	}
	for _, tc := range tests {
		if got := busFromPath(tc.id, tc.target); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.target, got, tc.want)
		}
	}
}
