package chunk

import (
	"testing"
	"time"
)

var dt0 = time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)

func diskRoundTrip(t *testing.T, rows []DiskRow) []DiskRow {
	t.Helper()
	blob, mounts, err := EncodeDisk(rows)
	if err != nil {
		t.Fatalf("EncodeDisk: %v", err)
	}
	got, err := DecodeDisk(blob)
	if err != nil {
		t.Fatalf("DecodeDisk: %v", err)
	}
	wantMounts := map[string]bool{}
	for _, r := range rows {
		wantMounts[r.Mount] = true
	}
	if mounts != len(wantMounts) {
		t.Errorf("mounts got %d, want %d", mounts, len(wantMounts))
	}
	return got
}

// Dos montajes multiplexados en el mismo host-dia vuelven completos y ordenados por ts y luego mount.
func TestDiskRoundTripMultiplexesMounts(t *testing.T) {
	rows := []DiskRow{
		{TS: dt0, Mount: "/", UsedMB: 1000, TotalMB: 500000},
		{TS: dt0, Mount: "/data", UsedMB: 2000, TotalMB: 1000000},
		{TS: dt0.Add(5 * time.Minute), Mount: "/", UsedMB: 1010, TotalMB: 500000},
		{TS: dt0.Add(5 * time.Minute), Mount: "/data", UsedMB: 2050, TotalMB: 1000000},
		{TS: dt0.Add(10 * time.Minute), Mount: "/", UsedMB: 1005, TotalMB: 500000},
	}
	got := diskRoundTrip(t, rows)
	want := []DiskRow{rows[0], rows[1], rows[2], rows[3], rows[4]}
	if len(got) != len(want) {
		t.Fatalf("got %d filas, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("fila %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// El orden de entrada no importa: Decode siempre entrega por ts y, a igual ts, por mount.
func TestDiskRoundTripOrdersOutput(t *testing.T) {
	rows := []DiskRow{
		{TS: dt0.Add(10 * time.Minute), Mount: "/data", UsedMB: 1, TotalMB: 10},
		{TS: dt0, Mount: "/data", UsedMB: 1, TotalMB: 10},
		{TS: dt0, Mount: "/", UsedMB: 1, TotalMB: 10},
	}
	got := diskRoundTrip(t, rows)
	if len(got) != 3 || got[0].Mount != "/" || got[1].Mount != "/data" || got[2].TS.Before(got[1].TS) {
		t.Fatalf("orden got %+v", got)
	}
}

func TestDiskEncodeIsDeterministic(t *testing.T) {
	rows := []DiskRow{{TS: dt0, Mount: "/", UsedMB: 1, TotalMB: 2}, {TS: dt0.Add(time.Minute), Mount: "/boot", UsedMB: 3, TotalMB: 4}}
	b1, _, err := EncodeDisk(rows)
	if err != nil {
		t.Fatal(err)
	}
	b2, _, err := EncodeDisk(rows)
	if err != nil {
		t.Fatal(err)
	}
	if string(b1) != string(b2) {
		t.Error("EncodeDisk no es deterministico")
	}
}

func TestDiskEncodeRejectsEmptyOrTooLongMount(t *testing.T) {
	for _, m := range []string{"", string(make([]byte, maxMountLen+1))} {
		if _, _, err := EncodeDisk([]DiskRow{{TS: dt0, Mount: m, UsedMB: 1, TotalMB: 2}}); err == nil {
			t.Errorf("mount %q: esperaba error", m)
		}
	}
}

// DecodeDisk detecta corrupcion: CRC alterado, version desconocida y datos truncados.
func TestDiskCorruptionIsDetected(t *testing.T) {
	blob, _, err := EncodeDisk([]DiskRow{{TS: dt0, Mount: "/", UsedMB: 5, TotalMB: 10}})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("crc alterado", func(t *testing.T) {
		bad := append([]byte{}, blob...)
		bad[len(bad)-1] ^= 0xFF
		if _, err := DecodeDisk(bad); err == nil {
			t.Error("esperaba error")
		}
	})
	t.Run("version desconocida", func(t *testing.T) {
		bad := append([]byte{}, blob...)
		bad[0] = 99
		if _, err := DecodeDisk(bad); err == nil {
			t.Error("esperaba error")
		}
	})
	t.Run("truncado", func(t *testing.T) {
		if _, err := DecodeDisk(blob[:diskHeaderLen+1]); err == nil {
			t.Error("esperaba error")
		}
	})
	t.Run("demasiado corto", func(t *testing.T) {
		if _, err := DecodeDisk(blob[:3]); err == nil {
			t.Error("esperaba error")
		}
	})
}

func TestDiskEncodeRejectsTooManyRows(t *testing.T) {
	rows := make([]DiskRow, MaxRows+1)
	for i := range rows {
		rows[i] = DiskRow{TS: dt0.Add(time.Duration(i) * time.Second), Mount: "/", UsedMB: 1, TotalMB: 2}
	}
	if _, _, err := EncodeDisk(rows); err == nil {
		t.Error("esperaba error de demasiadas filas")
	}
}
