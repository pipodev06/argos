package sender

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
)

const (
	// journalName es el archivo del buffer offline, junto a agent.json en el directorio de estado.
	journalName = "buffer.log"

	// DefaultMaxBuffer y DefaultMaxBufferBytes acotan el buffer offline (lo que llegue primero).
	DefaultMaxBuffer      = 2000
	DefaultMaxBufferBytes = 20 << 20

	// compactMinDead: no se compacta el archivo mientras lo muerto (ya enviado) pese menos que esto.
	compactMinDead = 1 << 20
	// journalSlack es el margen sobre el tope de bytes al leer el archivo (lo muerto aun no compactado).
	journalSlack = 24 << 20
)

// FileStore es lo que el buffer necesita del directorio de estado del agente (lo implementa
// *state.Store, que aplica los permisos restrictivos). Nil en Options = buffer solo en memoria.
type FileStore interface {
	ReadFile(name string, limit int64) ([]byte, error)
	WriteFile(name string, data []byte) error
	OpenAppend(name string) (*os.File, error)
}

// entry es un reporte pendiente; size es lo que ocupa su linea en el journal.
type entry struct {
	r    Report
	size int64
}

// queue es la cola FIFO acotada de reportes pendientes. Con FileStore se respalda en un journal
// append-only (buffer.log): cada linea es "R<json>" (reporte encolado) o "A" (el mas viejo salio,
// enviado o descartado). Al arrancar se repite el journal pa reconstruir la cola; si crece con
// lineas muertas se compacta con reemplazo atomico. Un journal ilegible se descarta entero.
// Los reportes no llevan el token (va en el header), asi que el archivo tampoco.
type queue struct {
	items    []entry
	live     int64 // suma de size de items
	maxItems int
	maxBytes int64

	store  FileStore // nil = solo memoria
	f      *os.File
	dead   int64 // bytes del journal que ya no son cola
	logger *slog.Logger
}

func newQueue(store FileStore, maxItems int, maxBytes int64, logger *slog.Logger) *queue {
	return &queue{store: store, maxItems: maxItems, maxBytes: maxBytes, logger: logger}
}

func (q *queue) len() int { return len(q.items) }

func (q *queue) head() *Report { return &q.items[0].r }

func journalLine(r Report) ([]byte, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("serializando reporte: %w", err)
	}
	line := make([]byte, 0, len(body)+2)
	line = append(line, 'R')
	line = append(line, body...)
	return append(line, '\n'), nil
}

// load reconstruye la cola desde el journal. Nunca falla: ante cualquier problema con el archivo
// lo descarta (con un log sin datos) y sigue con la cola vacia.
func (q *queue) load() {
	if q.store == nil {
		return
	}
	data, err := q.store.ReadFile(journalName, q.maxBytes+journalSlack)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return
	case err != nil:
		q.logger.Warn("no se pudo leer el buffer offline, se descarta", "err", err)
		q.reset()
		return
	case int64(len(data)) > q.maxBytes+journalSlack:
		q.logger.Warn("el buffer offline en disco excede su tope, se descarta", "bytes", len(data))
		q.reset()
		return
	}
	// Una ultima linea sin salto de linea es una escritura cortada (kill -9, disco lleno): se ignora.
	torn := int64(0)
	if i := bytes.LastIndexByte(data, '\n'); i < len(data)-1 {
		torn = int64(len(data) - (i + 1))
		data = data[:i+1]
	}
	for len(data) > 0 {
		line, rest, _ := bytes.Cut(data, []byte{'\n'})
		data = rest
		switch {
		case len(line) == 1 && line[0] == 'A':
			if len(q.items) > 0 {
				q.dead += q.items[0].size + 2
				q.live -= q.items[0].size
				q.items = q.items[1:]
			}
		case len(line) > 1 && line[0] == 'R':
			var r Report
			if err := json.Unmarshal(line[1:], &r); err != nil {
				q.logger.Warn("buffer offline corrupto, se descarta", "pendientes", len(q.items))
				q.reset()
				return
			}
			q.items = append(q.items, entry{r: r, size: int64(len(line) + 1)})
			q.live += int64(len(line) + 1)
		default:
			q.logger.Warn("buffer offline corrupto, se descarta", "pendientes", len(q.items))
			q.reset()
			return
		}
	}
	dropped := 0
	for len(q.items) > q.maxItems || (q.live > q.maxBytes && len(q.items) > 0) {
		q.items, q.live = q.items[1:], q.live-q.items[0].size
		dropped++
	}
	if dropped > 0 {
		q.logger.Warn("buffer offline sobre el tope al arrancar, se descartaron los mas viejos", "descartados", dropped)
	}
	if len(q.items) > 0 {
		q.logger.Info("buffer offline recuperado del disco", "pendientes", len(q.items))
	}
	if dropped > 0 || torn > 0 || q.dead > 0 {
		q.compact()
	}
}

// reset deja la cola y el archivo vacios.
func (q *queue) reset() {
	q.items, q.live, q.dead = nil, 0, 0
	q.compact()
}

// push encola r; si se pasa del tope (cantidad o bytes) descarta los mas viejos.
// Los datos ocasionales (discos, host_info) del descartado pasan al siguiente pa no perderlos.
func (q *queue) push(r Report) {
	line, err := journalLine(r)
	if err != nil {
		q.logger.Error("no se pudo encolar el reporte, se descarta", "err", err)
		return
	}
	for len(q.items) > 0 && (len(q.items) >= q.maxItems || q.live+int64(len(line)) > q.maxBytes) {
		q.logger.Warn("buffer offline lleno, se descarta el reporte mas viejo", "max", q.maxItems, "max_bytes", q.maxBytes)
		oldest := q.items[0].r
		q.pop()
		if len(q.items) > 0 {
			q.items[0].r.inheritExtras(oldest)
		} else {
			r.inheritExtras(oldest)
		}
	}
	if line, err = journalLine(r); err != nil || int64(len(line)) > q.maxBytes {
		q.logger.Error("reporte demasiado grande pa el buffer, se descarta", "bytes", len(line))
		return
	}
	q.items = append(q.items, entry{r: r, size: int64(len(line))})
	q.live += int64(len(line))
	q.write(line)
}

// pop saca el mas viejo (enviado o descartado).
func (q *queue) pop() {
	q.dead += q.items[0].size + 2
	q.live -= q.items[0].size
	q.items = q.items[1:]
	q.write([]byte("A\n"))
	if q.dead > compactMinDead && q.dead > q.live {
		q.compact()
	}
}

func (q *queue) open() bool {
	if q.store == nil {
		return false
	}
	if q.f != nil {
		return true
	}
	f, err := q.store.OpenAppend(journalName)
	if err != nil {
		q.logger.Warn("no se pudo abrir el buffer offline en disco; sigue solo en memoria", "err", err)
		return false
	}
	q.f = f
	return true
}

func (q *queue) write(b []byte) {
	if !q.open() {
		return
	}
	if _, err := q.f.Write(b); err != nil {
		q.logger.Warn("no se pudo escribir el buffer offline en disco; sigue solo en memoria", "err", err)
		q.closeFile() // se reabre (o falla otra vez con log) en la proxima escritura
	}
}

func (q *queue) closeFile() {
	if q.f != nil {
		_ = q.f.Close() // el error de escritura ya se logueo
		q.f = nil
	}
}

// compact reescribe el journal solo con lo pendiente (reemplazo atomico).
func (q *queue) compact() {
	if q.store == nil {
		return
	}
	q.closeFile() // en Windows no se puede reemplazar un archivo abierto
	var buf bytes.Buffer
	q.live = 0
	kept := q.items[:0]
	for _, e := range q.items {
		line, err := journalLine(e.r)
		if err != nil {
			continue
		}
		buf.Write(line)
		e.size = int64(len(line))
		q.live += e.size
		kept = append(kept, e)
	}
	q.items = kept
	if err := q.store.WriteFile(journalName, buf.Bytes()); err != nil {
		q.logger.Warn("no se pudo compactar el buffer offline en disco", "err", err)
		return
	}
	q.dead = 0
}

// close libera el archivo del journal.
func (q *queue) close() { q.closeFile() }
