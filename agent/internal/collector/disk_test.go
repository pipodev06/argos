package collector

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/shirou/gopsutil/v4/disk"
)

func part(device, mount, fs string) disk.PartitionStat {
	return disk.PartitionStat{Device: device, Mountpoint: mount, Fstype: fs}
}

func mounts(ps []disk.PartitionStat) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Mountpoint
	}
	return out
}

func TestSelectMounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		parts []disk.PartitionStat
		want  []string
	}{
		{
			name: "linux tipico: descarta pseudo-fs",
			parts: []disk.PartitionStat{
				part("/dev/nvme0n1p2", "/", "ext4"),
				part("tmpfs", "/run", "tmpfs"),
				part("devtmpfs", "/dev", "devtmpfs"),
				part("proc", "/proc", "proc"),
				part("sysfs", "/sys", "sysfs"),
				part("cgroup2", "/sys/fs/cgroup", "cgroup2"),
				part("overlay", "/var/lib/docker/overlay2/x/merged", "overlay"),
				part("/dev/loop3", "/snap/core/1", "squashfs"),
				part("/dev/nvme0n1p1", "/boot/efi", "vfat"),
				part("/dev/sdb1", "/mnt/datos", "ntfs3"),
				part("/dev/sdc1", "/mnt/ntfs-3g", "fuseblk"),
			},
			want: []string{"/", "/boot/efi", "/mnt/datos", "/mnt/ntfs-3g"},
		},
		{
			name: "bind mounts y subvolumenes: un solo montaje por dispositivo (el mas corto)",
			parts: []disk.PartitionStat{
				part("/dev/sda1", "/home", "btrfs"),
				part("/dev/sda1", "/", "btrfs"),
				part("/dev/sda1", "/var/log", "btrfs"),
				part("/dev/sdb1", "/srv/bind", "ext4"),
				part("/dev/sdb1", "/mnt/original", "ext4"),
			},
			want: []string{"/", "/srv/bind"},
		},
		{
			name: "red, fuse y opticos",
			parts: []disk.PartitionStat{
				part("/dev/sda1", "/", "ext4"),
				part("nas:/export", "/mnt/nas", "nfs4"),
				part("//srv/share", "/mnt/smb", "cifs"),
				part("sshfs#u@h:/", "/mnt/ssh", "fuse.sshfs"),
				part("gvfsd-fuse", "/run/user/1000/gvfs", "fuse.gvfsd-fuse"),
				part("/dev/sr0", "/media/cd", "iso9660"),
			},
			want: []string{"/"},
		},
		{
			name: "macos: solo / y el volumen Data",
			parts: []disk.PartitionStat{
				part("/dev/disk3s1s1", "/", "apfs"),
				part("/dev/disk3s5", "/System/Volumes/Data", "apfs"),
				part("/dev/disk3s6", "/System/Volumes/VM", "apfs"),
				part("/dev/disk3s2", "/System/Volumes/Preboot", "apfs"),
				part("devfs", "/dev", "devfs"),
				part("map auto_home", "/System/Volumes/Data/home", "autofs"),
				part("/dev/disk4s1", "/Volumes/Externo", "exfat"),
			},
			want: []string{"/", "/Volumes/Externo", "/System/Volumes/Data"},
		},
		{
			name: "windows: la particion vacia de unidades sin medio no cuenta",
			parts: []disk.PartitionStat{
				part("C:", "C:", "NTFS"),
				{},
				part("D:", "D:", "NTFS"),
				part("E:", "E:", "CDFS"),
			},
			want: []string{"C:", "D:"},
		},
		{
			name: "montajes invalidos",
			parts: []disk.PartitionStat{
				part("/dev/a", "/ok", "ext4"),
				part("/dev/b", "/nul\x00", "ext4"),
				part("/dev/c", "/\xff\xfe", "ext4"),
				part("/dev/d", "/"+strings.Repeat("x", 300), "ext4"),
			},
			want: []string{"/ok"},
		},
		{name: "nada", parts: nil, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mounts(selectMounts(tt.parts, nil))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectMountsCapsAtSixteenKeepingRootAndIsStable(t *testing.T) {
	t.Parallel()

	var parts []disk.PartitionStat
	for i := 30; i >= 1; i-- { // desordenados a proposito
		parts = append(parts, part(fmt.Sprintf("/dev/sd%d", i), fmt.Sprintf("/mnt/d%02d", i), "ext4"))
	}
	parts = append(parts, part("/dev/root", "/", "ext4"))

	first := mounts(selectMounts(parts, nil))
	if len(first) != maxDisks {
		t.Fatalf("montajes got %d, want %d", len(first), maxDisks)
	}
	if first[0] != "/" {
		t.Errorf("la raiz debe sobrevivir al limite, got %v", first)
	}
	// Mismo resultado sin importar el orden de entrada (evita que el conjunto reportado parpadee).
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	if second := mounts(selectMounts(parts, nil)); !reflect.DeepEqual(first, second) {
		t.Errorf("seleccion inestable:\n%v\n%v", first, second)
	}
}

func TestSelectMountsAppliesPlatformFilter(t *testing.T) {
	t.Parallel()

	parts := []disk.PartitionStat{part("C:", "C:", "NTFS"), part("Z:", "Z:", "NTFS")}
	got := mounts(selectMounts(parts, func(p disk.PartitionStat) bool { return p.Mountpoint != "Z:" }))
	if !reflect.DeepEqual(got, []string{"C:"}) {
		t.Errorf("got %v, want [C:]", got)
	}
}

func TestDisksUsageInMiBSkipsBrokenMountsAndBadTotals(t *testing.T) {
	t.Parallel()

	s := NewSystem(nil)
	s.keepMount = nil
	s.partitions = func(context.Context) ([]disk.PartitionStat, error) {
		return []disk.PartitionStat{
			part("/dev/a", "/", "ext4"),
			part("/dev/b", "/roto", "ext4"),
			part("/dev/c", "/vacio", "ext4"),
			part("/dev/d", "/inconsistente", "ext4"),
			part("/dev/e", "/pequeno", "ext4"),
		}, nil
	}
	s.usage = func(_ context.Context, path string) (*disk.UsageStat, error) {
		switch path {
		case "/":
			return &disk.UsageStat{Total: 200000 << 20, Used: 50000<<20 + 123}, nil
		case "/roto":
			return nil, errors.New("statfs: input/output error")
		case "/vacio":
			return &disk.UsageStat{Total: 0, Used: 0}, nil
		case "/inconsistente":
			return &disk.UsageStat{Total: 100 << 20, Used: 900 << 20}, nil
		default:
			return &disk.UsageStat{Total: 512, Used: 10}, nil // < 1 MiB: total_mb seria 0
		}
	}
	got, err := s.Disks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]int64{"/": {50000, 200000}, "/inconsistente": {100, 100}}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want solo %v", got, want)
	}
	for _, d := range got {
		w, ok := want[d.Mount]
		if !ok || d.UsedMB != w[0] || d.TotalMB != w[1] {
			t.Errorf("disco %+v, want used/total %v", d, w)
		}
	}
}

func TestDisksToleratesGopsutilWarningsButNotEmptyListing(t *testing.T) {
	t.Parallel()

	s := NewSystem(nil)
	s.keepMount = nil
	s.usage = func(context.Context, string) (*disk.UsageStat, error) {
		return &disk.UsageStat{Total: 10 << 20, Used: 1 << 20}, nil
	}
	s.partitions = func(context.Context) ([]disk.PartitionStat, error) {
		return []disk.PartitionStat{part("C:", "C:", "NTFS")}, errors.New("aviso: unidad Z: no lista")
	}
	got, err := s.Disks(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("con aviso y datos: got %+v err %v, want 1 disco sin error", got, err)
	}
	s.partitions = func(context.Context) ([]disk.PartitionStat, error) { return nil, errors.New("sin acceso") }
	if _, err := s.Disks(context.Background()); err == nil {
		t.Error("sin particiones y con error se esperaba error")
	}
}
