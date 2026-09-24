// Package quiet evita el spam de logs por lecturas opcionales que fallan en
// cada ciclo: registra solo el cambio de estado (empieza a fallar / se recupera).
package quiet

import (
	"context"
	"log/slog"
)

// Tracker recuerda que lecturas estan fallando. NO es seguro pa uso concurrente:
// un solo dueno (el loop del agente).
type Tracker struct {
	logger  *slog.Logger
	failing map[string]bool
}

// New crea un Tracker; con logger nil no registra nada.
func New(logger *slog.Logger) *Tracker {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Tracker{logger: logger, failing: map[string]bool{}}
}

// Fail registra el fallo de la lectura key solo la primera vez que falla seguida.
func (t *Tracker) Fail(ctx context.Context, key string, err error) {
	if t.failing[key] {
		return
	}
	t.failing[key] = true
	t.logger.InfoContext(ctx, "lectura opcional fallida, se omite mientras siga fallando", "lectura", key, "err", err)
}

// OK marca la lectura key como sana y avisa una vez si se habia recuperado.
func (t *Tracker) OK(ctx context.Context, key string) {
	if !t.failing[key] {
		return
	}
	delete(t.failing, key)
	t.logger.InfoContext(ctx, "lectura opcional recuperada", "lectura", key)
}
