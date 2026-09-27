// Codec de disk_samples: mucho menos volumen que metrics (un reporte cada pocos minutos, pocos montajes por
// host), asi que no reusa el formato columnar de Row (pensado pa las 12 columnas opcionales de metrics). Un
// host-dia multiplexa sus montajes: cada uno guarda su propia serie de tiempo (delta-de-delta) y sus columnas
// used_mb/total_mb (delta+zigzag+varint; nunca NULL, a diferencia de metrics), todo flate nivel 9 con el mismo
// CRC32 del contenido sin comprimir. Ver internal/retention/pack_disk.go (empaquetador) y decisiones/2026-09-26.
package chunk

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"sort"
	"time"
)

// DiskVersion es la version del formato (y el valor de disk_chunks.codec). Espacio de versiones propio,
// independiente del de metrics_chunks (Version): son tablas y codecs distintos.
const DiskVersion = 1

const (
	// diskHeaderLen: version (1) + n (4, little endian) + CRC32 del contenido sin comprimir (4).
	diskHeaderLen = 9

	// maxMountLen es el limite de disk_samples.mount (CHECK de la migracion 00010): defensivo en Decode.
	maxMountLen = 128

	// maxDiskRowBytes acota los bytes sin comprimir por fila (mount + 3 varints de hasta 10 B): protege a
	// DecodeDisk de cabeceras absurdas. Se cuenta una vez por fila aunque el mount solo se repita por grupo.
	maxDiskRowBytes = maxMountLen + 32
)

// DiskRow es una muestra de disk_samples. A diferencia de Row, ninguna columna es NULL (la tabla no lo permite).
type DiskRow struct {
	TS      time.Time
	Mount   string
	UsedMB  int32
	TotalMB int32
}

// EncodeDisk empaqueta las muestras de un host-dia (idealmente ordenadas por mount y ts; el orden de salida de
// Decode siempre es por ts y luego mount, sin importar el de entrada). Devuelve el blob y el numero de montajes
// distintos (pa la columna disk_chunks.mount_count).
func EncodeDisk(rows []DiskRow) ([]byte, int, error) {
	n := len(rows)
	if n > MaxRows {
		return nil, 0, fmt.Errorf("%d filas: %w", n, ErrTooManyRows)
	}
	order, groups := groupDiskRows(rows)
	for _, m := range order {
		if l := len(m); l == 0 || l > maxMountLen {
			return nil, 0, fmt.Errorf("montaje de %d bytes: %w", l, ErrCorrupt)
		}
	}
	payload := marshalDiskPayload(order, groups)

	var buf bytes.Buffer
	var hdr [diskHeaderLen]byte
	hdr[0] = DiskVersion
	binary.LittleEndian.PutUint32(hdr[1:5], uint32(n))
	binary.LittleEndian.PutUint32(hdr[5:9], crc32.ChecksumIEEE(payload))
	buf.Write(hdr[:])
	fw, err := flate.NewWriter(&buf, flateLevel)
	if err != nil {
		return nil, 0, fmt.Errorf("creando el compresor: %w", err)
	}
	if _, err := fw.Write(payload); err != nil {
		return nil, 0, fmt.Errorf("comprimiendo: %w", err)
	}
	if err := fw.Close(); err != nil {
		return nil, 0, fmt.Errorf("cerrando el compresor: %w", err)
	}
	return buf.Bytes(), len(order), nil
}

// groupDiskRows agrupa las filas por montaje (orden de primera aparicion) y las ordena por ts dentro de cada grupo.
func groupDiskRows(rows []DiskRow) ([]string, map[string][]DiskRow) {
	groups := make(map[string][]DiskRow, 4)
	var order []string
	for _, r := range rows {
		if _, ok := groups[r.Mount]; !ok {
			order = append(order, r.Mount)
		}
		groups[r.Mount] = append(groups[r.Mount], r)
	}
	for _, m := range order {
		g := groups[m]
		sort.Slice(g, func(i, j int) bool { return g[i].TS.Before(g[j].TS) })
	}
	return order, groups
}

// marshalDiskPayload arma el contenido sin comprimir: numero de montajes y, por cada uno, su nombre, cuantas
// filas trae y sus tres series (ts, used_mb, total_mb).
func marshalDiskPayload(order []string, groups map[string][]DiskRow) []byte {
	b := make([]byte, 0, 64)
	b = binary.AppendUvarint(b, uint64(len(order)))
	for _, mount := range order {
		rows := groups[mount]
		mb := []byte(mount)
		b = binary.AppendUvarint(b, uint64(len(mb)))
		b = append(b, mb...)
		b = binary.AppendUvarint(b, uint64(len(rows)))
		ts := make([]int64, len(rows))
		used := make([]int64, len(rows))
		total := make([]int64, len(rows))
		for i, r := range rows {
			ts[i], used[i], total[i] = r.TS.UnixMicro(), int64(r.UsedMB), int64(r.TotalMB)
		}
		b = appendDeltaTS(b, ts)
		b = appendDeltaVarint(b, used)
		b = appendDeltaVarint(b, total)
	}
	return b
}

// DecodeDisk desempaqueta un blob de EncodeDisk. Verifica el CRC32 y todos los rangos; devuelve las filas
// ordenadas por ts y, a igual ts, por mount.
func DecodeDisk(data []byte) ([]DiskRow, error) {
	n, want, err := parseDiskHeader(data)
	if err != nil {
		return nil, err
	}
	limit := int64(n)*maxDiskRowBytes + 4096
	payload, err := io.ReadAll(io.LimitReader(flate.NewReader(bytes.NewReader(data[diskHeaderLen:])), limit+1))
	if err != nil {
		return nil, fmt.Errorf("descomprimiendo: %w", errors.Join(ErrCorrupt, err))
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("contenido mayor que el tope: %w", ErrCorrupt)
	}
	if crc32.ChecksumIEEE(payload) != want {
		return nil, fmt.Errorf("crc32 distinto: %w", ErrCorrupt)
	}
	return unmarshalDiskPayload(payload, n)
}

func parseDiskHeader(data []byte) (n int, crc uint32, err error) {
	if len(data) < diskHeaderLen {
		return 0, 0, fmt.Errorf("cabecera de %d bytes: %w", len(data), ErrCorrupt)
	}
	if data[0] != DiskVersion {
		return 0, 0, fmt.Errorf("version %d: %w", data[0], ErrVersion)
	}
	n32 := binary.LittleEndian.Uint32(data[1:5])
	if n32 > MaxRows {
		return 0, 0, fmt.Errorf("%d filas: %w", n32, ErrCorrupt)
	}
	return int(n32), binary.LittleEndian.Uint32(data[5:9]), nil
}

func unmarshalDiskPayload(payload []byte, n int) ([]DiskRow, error) {
	r := &reader{b: payload}
	numMounts := r.uvarint()
	if r.err != nil {
		return nil, r.err
	}
	if numMounts > uint64(n)+1 { //nolint:gosec // n sale de parseHeader ya acotado a MaxRows: no se desborda
		return nil, fmt.Errorf("%d montajes pa %d filas: %w", numMounts, n, ErrCorrupt)
	}
	rows := make([]DiskRow, 0, n)
	remaining := n
	for range numMounts {
		mlen := r.uvarint()
		if r.err != nil {
			return nil, r.err
		}
		//nolint:gosec // mlen <= maxMountLen (128) tras el chequeo: int(mlen) no se desborda
		if mlen == 0 || mlen > maxMountLen || int(mlen) > len(r.b) {
			return nil, fmt.Errorf("montaje de %d bytes: %w", mlen, ErrCorrupt)
		}
		mount := string(r.b[:mlen])
		r.b = r.b[mlen:]

		cnt := r.uvarint()
		if r.err != nil {
			return nil, r.err
		}
		// La comparacion va en uint64 (remaining siempre es >= 0 y <= n): cnt <= remaining queda acotado
		// a MaxRows antes de convertirlo a int, así int(cnt) nunca se desborda.
		if cnt == 0 || cnt > uint64(remaining) { //nolint:gosec // remaining >= 0 <= MaxRows
			return nil, fmt.Errorf("%d filas de %q exceden las restantes: %w", cnt, mount, ErrCorrupt)
		}
		c := int(cnt) //nolint:gosec // cnt <= remaining <= MaxRows (1<<20): cabe en int
		ts := readDeltaTS(r, c)
		used := readDeltaVarint(r, c)
		total := readDeltaVarint(r, c)
		if r.err != nil {
			return nil, r.err
		}
		for i := range c {
			if used[i] < 0 || used[i] > math.MaxInt32 || total[i] <= 0 || total[i] > math.MaxInt32 {
				return nil, fmt.Errorf("valor de disco fuera de rango: %w", ErrCorrupt)
			}
			rows = append(rows, DiskRow{TS: time.UnixMicro(ts[i]).UTC(), Mount: mount, UsedMB: int32(used[i]), TotalMB: int32(total[i])})
		}
		remaining -= c
	}
	if remaining != 0 {
		return nil, fmt.Errorf("%d filas sin montaje: %w", remaining, ErrCorrupt)
	}
	if len(r.b) != 0 {
		return nil, fmt.Errorf("%d bytes sobrantes: %w", len(r.b), ErrCorrupt)
	}
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].TS.Equal(rows[j].TS) {
			return rows[i].TS.Before(rows[j].TS)
		}
		return rows[i].Mount < rows[j].Mount
	})
	return rows, nil
}
