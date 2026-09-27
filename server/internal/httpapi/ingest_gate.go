package httpapi

import (
	"sync"
	"time"
)

const (
	// minSampleGap es la separacion minima entre los `ts` de dos muestras aceptadas del mismo host. El agente no deja
	// un ARGOS_INTERVAL menor a 5 s (agent/internal/config, minInterval); el `ts` del reporte se trunca a segundos, asi
	// que dos muestras legitimas consecutivas quedan a >= 4 s. Se descarta lo que este a MENOS de 4 s: un token robado
	// no puede escribir mas de ~1 muestra cada 4 s por host (antes ~15x la frecuencia normal de 30 s).
	minSampleGap = 4 * time.Second
	// maxGateHosts acota la memoria del registro; al llenarse se vacia (falla abierto: solo se pierde el tope un rato).
	maxGateHosts = 100_000
	// dropLogEvery agrega los descartes en una sola linea de log por ventana.
	dropLogEvery = time.Minute
)

// ingestGate recuerda el `ts` mas reciente ya aceptado de cada host, en memoria. Se compara por `ts` de la muestra y
// no por hora de llegada: el buffer offline del agente reenvia reportes historicos con `ts` espaciados por el
// intervalo, y esos deben pasar. Un reinicio lo vacia (aceptado: solo cuesta el tope unos segundos).
type ingestGate struct {
	mu      sync.Mutex
	last    map[string]time.Time
	dropped int
	since   time.Time
}

// tooSoon dice si ts esta a menos de minSampleGap de la muestra ya aceptada del host (antes o despues de ella).
func (g *ingestGate) tooSoon(hostID string, ts time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	last, ok := g.last[hostID]
	if !ok {
		return false
	}
	gap := ts.Sub(last)
	if gap < 0 {
		gap = -gap
	}
	return gap < minSampleGap
}

// accepted anota que la muestra con ese ts se guardo (se conserva el mayor).
func (g *ingestGate) accepted(hostID string, ts time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.last == nil || len(g.last) >= maxGateHosts {
		if _, known := g.last[hostID]; !known {
			g.last = make(map[string]time.Time)
		}
	}
	if cur, ok := g.last[hostID]; !ok || ts.After(cur) {
		g.last[hostID] = ts
	}
}

// noteDrop cuenta un descarte. Devuelve cuantos se acumularon cuando ya toca loguearlos (una vez por dropLogEvery),
// y 0 el resto de las veces.
func (g *ingestGate) noteDrop(now time.Time) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.since.IsZero() {
		g.since = now
	}
	g.dropped++
	if now.Sub(g.since) < dropLogEvery {
		return 0
	}
	n := g.dropped
	g.dropped, g.since = 0, now
	return n
}
