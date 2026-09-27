// Package beat guarda en memoria el resultado del ultimo ciclo de una tarea periodica (mantenimiento, evaluador
// de alertas) pa que GET /system/status pueda decir si sigue viva. No depende de ningun otro paquete de argos.
package beat

import (
	"sync"
	"time"
)

// State es una copia consistente del estado de un Beat.
type State struct {
	// LastOK es el fin del ultimo ciclo sin error (cero = ninguno desde el arranque).
	LastOK time.Time
	// LastFail es el fin del ultimo ciclo con error (cero = ninguno).
	LastFail time.Time
	// LastErr es el texto del ultimo error. Es interno: solo lo ve un admin y nunca lleva secretos (son errores
	// de BD ya envueltos).
	LastErr string
}

// Beat registra los ciclos de una tarea. El valor cero esta listo pa usar; un *Beat nil ignora todo (los tests y
// las piezas sin auto-monitoreo no necesitan uno).
type Beat struct {
	mu sync.Mutex
	st State
}

// OK anota un ciclo terminado sin error en el instante at.
func (b *Beat) OK(at time.Time) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.st.LastOK = at
}

// Fail anota un ciclo con error en el instante at.
func (b *Beat) Fail(at time.Time, err error) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.st.LastFail = at
	if err != nil {
		b.st.LastErr = err.Error()
	}
}

// Snapshot devuelve el estado actual (cero si b es nil).
func (b *Beat) Snapshot() State {
	if b == nil {
		return State{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.st
}
