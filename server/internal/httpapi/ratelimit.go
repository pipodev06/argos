package httpapi

import (
	"math"
	"net/netip"
	"sync"
	"time"
)

// maxLimiterKeys acota la memoria del limitador ante un flood de claves distintas.
const maxLimiterKeys = 100_000

// limiter es un token bucket por clave, en memoria. Cada clave tiene burst tokens
// y recupera rate tokens por segundo. No persiste: un reinicio lo vacia (aceptado).
type limiter struct {
	mu        sync.Mutex
	rate      float64 // tokens por segundo
	burst     float64
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newLimiter(burst int, per time.Duration) *limiter {
	return &limiter{
		rate:    float64(burst) / per.Seconds(),
		burst:   float64(burst),
		buckets: make(map[string]*bucket),
	}
}

// allow consume un token de key. Si no hay, devuelve false y cuanto falta pa el proximo.
func (l *limiter) allow(key string, now time.Time) (ok bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)

	b, found := l.buckets[key]
	if !found {
		if len(l.buckets) >= maxLimiterKeys {
			for k := range l.buckets { // descarta una clave cualquiera
				delete(l.buckets, k)
				break
			}
		}
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens = math.Min(l.burst, b.tokens+elapsed*l.rate)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
	return false, wait
}

// sweep elimina, como mucho una vez por minuto, los buckets que ya se recuperaron por completo.
func (l *limiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*l.rate >= l.burst {
			delete(l.buckets, k)
		}
	}
}

// ipKey es la clave de rate limit de una IP: IPv6 se agrupa por /64 (un cliente
// controla todo su /64, si no rotaria de IP a voluntad).
func ipKey(ip netip.Addr) string {
	if !ip.IsValid() {
		return ""
	}
	ip = ip.Unmap()
	if ip.Is6() {
		p, err := ip.Prefix(64)
		if err == nil {
			return p.String()
		}
	}
	return ip.String()
}
