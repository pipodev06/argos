// Package chunk empaqueta las muestras de `metrics` de un host y un dia UTC en un solo blob comprimido
// (tabla metrics_chunks). El formato es columnar: cada columna se guarda aparte, con mapa de presencia
// (NULL) por rachas, deltas (delta-de-delta pa el tiempo), zigzag + varint y, al final, deflate nivel 9.
// Medido con metricas reales cada 10-30 s: ~8 B por muestra contra 178 B de una fila con su indice.
//
// Garantias:
//   - Determinista: las mismas filas dan los mismos bytes (con la misma version de Go).
//   - Sin perdidas salvo los flotantes de 32 bits (cpu, load), que se redondean a 2 decimales (Round2), y los
//     valores absurdos: NaN/Inf se guardan como NULL y |valor| > maxQuant/100 se recorta.
//   - Decode nunca entra en panico con bytes arbitrarios: un blob alterado se detecta por el CRC32 del contenido.
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
	"time"
)

// Version es la version del formato (y el valor de metrics_chunks.codec).
const Version = 1

const (
	// headerLen: version (1) + n (4, little endian) + CRC32 del contenido sin comprimir (4).
	headerLen = 9

	// MaxRows acota las filas de un chunk (un dia a 1 muestra por segundo son 86400): protege a Decode de
	// cabeceras absurdas.
	MaxRows = 1 << 20

	// maxRowBytes acota los bytes sin comprimir por fila (13 columnas de hasta 10 bytes de varint + presencia).
	maxRowBytes = 160

	// maxQuant es el tope de los flotantes cuantizados (x100): 1e15 cabe de sobra en int64 y en un float64 exacto.
	maxQuant = 1e15

	// flateLevel: el maximo; el empaquetado corre en el mantenimiento, no en el camino caliente.
	flateLevel = flate.BestCompression
)

// ErrCorrupt indica un blob mal formado, truncado o alterado (CRC distinto).
var ErrCorrupt = errors.New("chunk corrupto")

// ErrVersion indica un blob de una version del formato que este binario no conoce.
var ErrVersion = errors.New("version de chunk desconocida")

// ErrTooManyRows indica que Encode recibio mas de MaxRows filas.
var ErrTooManyRows = errors.New("demasiadas filas pa un chunk")

// Row es una muestra de metrics. Los tipos son los de las columnas de Postgres; nil = NULL.
type Row struct {
	TS              time.Time
	CPUPct          *float32
	MemUsedMB       *int32
	MemTotalMB      *int32
	NetRxBytes      *int64
	NetTxBytes      *int64
	SwapUsedMB      *int32
	SwapTotalMB     *int32
	Load1           *float32
	Load5           *float32
	Load15          *float32
	BatteryPct      *int16
	BatteryCharging *bool
}

// Round2 es el valor que devuelve Decode pa un flotante guardado: redondeado a 2 decimales.
func Round2(v float32) float32 {
	q, ok := quantize(v)
	if !ok {
		return v
	}
	return dequantize(q)
}

func quantize(v float32) (int64, bool) {
	f := float64(v)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	q := math.Round(f * 100)
	q = math.Max(-maxQuant, math.Min(maxQuant, q))
	return int64(q), true
}

func dequantize(q int64) float32 { return float32(float64(q) / 100) }

// col es una columna lista pa codificar: present[i] dice si la fila i tiene valor y vals[i] es ese valor.
type col struct {
	present []bool
	vals    []int64
}

func newCol(n int) col { return col{present: make([]bool, n), vals: make([]int64, n)} }

func (c *col) set(i int, v int64) { c.present[i], c.vals[i] = true, v }

func (c *col) setF32(i int, p *float32) {
	if p == nil {
		return
	}
	if q, ok := quantize(*p); ok {
		c.set(i, q)
	}
}

func (c *col) setI32(i int, p *int32) {
	if p != nil {
		c.set(i, int64(*p))
	}
}

func (c *col) setI64(i int, p *int64) {
	if p != nil {
		c.set(i, *p)
	}
}

// Numero de columnas opcionales, en el orden en que se escriben.
const (
	cCPU = iota
	cMemU
	cMemT
	cRx
	cTx
	cSwapU
	cSwapT
	cLoad1
	cLoad5
	cLoad15
	cBatPct
	cBatChg
	numCols
)

// Encode empaqueta filas (idealmente ordenadas por ts; el orden se conserva tal cual) en un blob.
func Encode(rows []Row) ([]byte, error) {
	n := len(rows)
	if n > MaxRows {
		return nil, fmt.Errorf("%d filas: %w", n, ErrTooManyRows)
	}
	ts := make([]int64, n)
	cols := make([]col, numCols)
	for i := range cols {
		cols[i] = newCol(n)
	}
	for i, r := range rows {
		ts[i] = r.TS.UnixMicro()
		cols[cCPU].setF32(i, r.CPUPct)
		cols[cMemU].setI32(i, r.MemUsedMB)
		cols[cMemT].setI32(i, r.MemTotalMB)
		cols[cRx].setI64(i, r.NetRxBytes)
		cols[cTx].setI64(i, r.NetTxBytes)
		cols[cSwapU].setI32(i, r.SwapUsedMB)
		cols[cSwapT].setI32(i, r.SwapTotalMB)
		cols[cLoad1].setF32(i, r.Load1)
		cols[cLoad5].setF32(i, r.Load5)
		cols[cLoad15].setF32(i, r.Load15)
		if r.BatteryPct != nil {
			cols[cBatPct].set(i, int64(*r.BatteryPct))
		}
		if r.BatteryCharging != nil {
			var v int64
			if *r.BatteryCharging {
				v = 1
			}
			cols[cBatChg].set(i, v)
		}
	}
	payload := marshalPayload(ts, cols)

	var buf bytes.Buffer
	var hdr [headerLen]byte
	hdr[0] = Version
	binary.LittleEndian.PutUint32(hdr[1:5], uint32(n))
	binary.LittleEndian.PutUint32(hdr[5:9], crc32.ChecksumIEEE(payload))
	buf.Write(hdr[:])
	fw, err := flate.NewWriter(&buf, flateLevel)
	if err != nil {
		return nil, fmt.Errorf("creando el compresor: %w", err)
	}
	if _, err := fw.Write(payload); err != nil {
		return nil, fmt.Errorf("comprimiendo: %w", err)
	}
	if err := fw.Close(); err != nil {
		return nil, fmt.Errorf("cerrando el compresor: %w", err)
	}
	return buf.Bytes(), nil
}

// marshalPayload arma el contenido sin comprimir: la columna de tiempo (delta-de-delta) y las opcionales.
func marshalPayload(ts []int64, cols []col) []byte {
	b := make([]byte, 0, len(ts)*8+64)
	var prev, prevDelta int64
	for _, t := range ts {
		delta := t - prev
		b = binary.AppendUvarint(b, zigzag(delta-prevDelta))
		prev, prevDelta = t, delta
	}
	for _, c := range cols {
		b = appendPresence(b, c.present)
		var last int64
		for i, ok := range c.present {
			if ok {
				b = binary.AppendUvarint(b, zigzag(c.vals[i]-last))
				last = c.vals[i]
			}
		}
	}
	return b
}

// appendPresence escribe el mapa de presencia como rachas alternas que empiezan por "presente" (la primera
// puede ser 0): uvarint(numero de rachas) y la longitud de cada una. Sin filas no hay rachas.
func appendPresence(b []byte, present []bool) []byte {
	if len(present) == 0 {
		return binary.AppendUvarint(b, 0)
	}
	var runs []uint64
	cur, length := true, uint64(0)
	for _, p := range present {
		if p != cur {
			runs = append(runs, length)
			cur, length = p, 0
		}
		length++
	}
	runs = append(runs, length)
	b = binary.AppendUvarint(b, uint64(len(runs)))
	for _, r := range runs {
		b = binary.AppendUvarint(b, r)
	}
	return b
}

// zigzag mapea enteros con signo a sin signo (0, -1, 1, -2 ... -> 0, 1, 2, 3 ...): la conversion es intencional (mismos bits).
func zigzag(v int64) uint64 { return uint64(v<<1) ^ uint64(v>>63) } //nolint:gosec // reinterpretacion de bits, sin perdida

func unzigzag(u uint64) int64 { return int64(u>>1) ^ -int64(u&1) }

// Decode desempaqueta un blob de Encode. Verifica el CRC32 del contenido y todos los rangos.
func Decode(data []byte) ([]Row, error) {
	n, want, err := parseHeader(data)
	if err != nil {
		return nil, err
	}
	limit := int64(n)*maxRowBytes + 4096
	payload, err := io.ReadAll(io.LimitReader(flate.NewReader(bytes.NewReader(data[headerLen:])), limit+1))
	if err != nil {
		return nil, fmt.Errorf("descomprimiendo: %w", errors.Join(ErrCorrupt, err))
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("contenido mayor que el tope: %w", ErrCorrupt)
	}
	if crc32.ChecksumIEEE(payload) != want {
		return nil, fmt.Errorf("crc32 distinto: %w", ErrCorrupt)
	}
	return unmarshalPayload(payload, n)
}

// Count devuelve el numero de filas de un blob sin descomprimirlo (solo lee la cabecera).
func Count(data []byte) (int, error) {
	n, _, err := parseHeader(data)
	return n, err
}

func parseHeader(data []byte) (n int, crc uint32, err error) {
	if len(data) < headerLen {
		return 0, 0, fmt.Errorf("cabecera de %d bytes: %w", len(data), ErrCorrupt)
	}
	if data[0] != Version {
		return 0, 0, fmt.Errorf("version %d: %w", data[0], ErrVersion)
	}
	n32 := binary.LittleEndian.Uint32(data[1:5])
	if n32 > MaxRows {
		return 0, 0, fmt.Errorf("%d filas: %w", n32, ErrCorrupt)
	}
	return int(n32), binary.LittleEndian.Uint32(data[5:9]), nil
}

// reader lee varints de un slice sin salirse de el.
type reader struct {
	b   []byte
	err error
}

func (r *reader) uvarint() uint64 {
	if r.err != nil {
		return 0
	}
	v, k := binary.Uvarint(r.b)
	if k <= 0 {
		r.err = fmt.Errorf("varint invalido: %w", ErrCorrupt)
		return 0
	}
	r.b = r.b[k:]
	return v
}

func unmarshalPayload(payload []byte, n int) ([]Row, error) {
	r := &reader{b: payload}
	rows := make([]Row, n)
	var prev, prevDelta int64
	for i := range rows {
		delta := prevDelta + unzigzag(r.uvarint())
		prev += delta
		prevDelta = delta
		rows[i].TS = time.UnixMicro(prev).UTC()
	}
	for c := range numCols {
		present, err := readPresence(r, n)
		if err != nil {
			return nil, err
		}
		var last int64
		for i, ok := range present {
			if !ok {
				continue
			}
			last += unzigzag(r.uvarint())
			if r.err != nil {
				return nil, r.err
			}
			if err := setField(&rows[i], c, last); err != nil {
				return nil, err
			}
		}
	}
	if r.err != nil {
		return nil, r.err
	}
	if len(r.b) != 0 {
		return nil, fmt.Errorf("%d bytes sobrantes: %w", len(r.b), ErrCorrupt)
	}
	return rows, nil
}

// readPresence lee un mapa de presencia de n filas; las rachas deben sumar exactamente n.
func readPresence(r *reader, n int) ([]bool, error) {
	runs := r.uvarint()
	if r.err != nil {
		return nil, r.err
	}
	if (n == 0 && runs != 0) || (n > 0 && (runs == 0 || runs > uint64(n)+1)) {
		return nil, fmt.Errorf("%d rachas pa %d filas: %w", runs, n, ErrCorrupt)
	}
	present := make([]bool, 0, n)
	cur := true
	for range runs {
		length := r.uvarint()
		if r.err != nil {
			return nil, r.err
		}
		if length > uint64(n-len(present)) { //nolint:gosec // len(present) <= n: la resta no es negativa
			return nil, fmt.Errorf("racha excede las filas: %w", ErrCorrupt)
		}
		for range length {
			present = append(present, cur)
		}
		cur = !cur
	}
	if len(present) != n {
		return nil, fmt.Errorf("presencia de %d filas, se esperaban %d: %w", len(present), n, ErrCorrupt)
	}
	return present, nil
}

// setField asigna el valor decodificado de la columna c a la fila, validando el rango del tipo destino.
func setField(row *Row, c int, v int64) error {
	switch c {
	case cCPU:
		row.CPUPct = f32(v)
	case cLoad1:
		row.Load1 = f32(v)
	case cLoad5:
		row.Load5 = f32(v)
	case cLoad15:
		row.Load15 = f32(v)
	case cMemU, cMemT, cSwapU, cSwapT:
		if v < math.MinInt32 || v > math.MaxInt32 {
			return fmt.Errorf("valor %d fuera de int32: %w", v, ErrCorrupt)
		}
		x := int32(v)
		switch c {
		case cMemU:
			row.MemUsedMB = &x
		case cMemT:
			row.MemTotalMB = &x
		case cSwapU:
			row.SwapUsedMB = &x
		default:
			row.SwapTotalMB = &x
		}
	case cRx:
		row.NetRxBytes = &v
	case cTx:
		row.NetTxBytes = &v
	case cBatPct:
		if v < math.MinInt16 || v > math.MaxInt16 {
			return fmt.Errorf("valor %d fuera de int16: %w", v, ErrCorrupt)
		}
		x := int16(v)
		row.BatteryPct = &x
	case cBatChg:
		if v != 0 && v != 1 {
			return fmt.Errorf("booleano %d: %w", v, ErrCorrupt)
		}
		x := v == 1
		row.BatteryCharging = &x
	}
	return nil
}

func f32(q int64) *float32 {
	x := dequantize(q)
	return &x
}
