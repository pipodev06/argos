//go:build windows

package collector

import (
	"context"
	"testing"
)

// TestReadPhysicalDisksReal corre contra el equipo real sin elevar: solo comprueba
// invariantes y registra lo que devuelve (usar -v).
func TestReadPhysicalDisksReal(t *testing.T) {
	s := NewSystem(nil)
	parts, err := s.partitions(context.Background())
	if err != nil && len(parts) == 0 {
		t.Skipf("sin particiones: %v", err)
	}
	sel := selectMounts(parts, s.keepMount)
	got := readPhysicalDisks(context.Background(), sel)
	if len(got) == 0 {
		t.Fatal("sin discos fisicos en un Windows real con unidades fijas")
	}
	reported := map[string]bool{}
	for _, p := range sel {
		reported[p.Mountpoint] = true
	}
	for _, d := range got {
		t.Logf("disco %s model=%q bus=%s size_mb=%d mounts=%v", d.ID, d.Model, d.Bus, d.SizeMB, d.Mounts)
		for _, m := range d.Mounts {
			if !reported[m] {
				t.Errorf("mount %q no es uno de los reportados por Disks", m)
			}
		}
	}
}
