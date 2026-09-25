package inventory

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// cmdSpec describe un comando de SOLO LECTURA: ruta absoluta, argv fijo (sin shell ni datos
// externos), entorno minimo, timeout duro y tope de salida.
type cmdSpec struct {
	path    string
	args    []string
	env     []string
	timeout time.Duration
	maxOut  int
}

// cmdResult es lo que dejo el comando: salida acotada y codigo de salida.
type cmdResult struct {
	out       []byte
	code      int
	truncated bool // la salida excedio maxOut (el resto se descarto)
}

type runFunc func(ctx context.Context, c cmdSpec) (cmdResult, error)

// capWriter guarda hasta max bytes y descarta el resto (sin bloquear al hijo).
type capWriter struct {
	buf       []byte
	max       int
	truncated bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	room := w.max - len(w.buf)
	if room > 0 {
		w.buf = append(w.buf, p[:min(room, len(p))]...)
	}
	if len(p) > room {
		w.truncated = true
	}
	return len(p), nil
}

// runCmd ejecuta c sin shell. Un codigo de salida distinto de 0 no es error (lo interpreta
// el parser); es error no poder arrancar, el timeout o la cancelacion. stderr se descarta.
func runCmd(ctx context.Context, c cmdSpec) (cmdResult, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	//nolint:gosec // G204: la ruta y el argv son constantes del paquete, nunca datos externos
	cmd := exec.CommandContext(ctx, c.path, c.args...)
	cmd.Env = c.env
	cmd.WaitDelay = 2 * time.Second
	w := &capWriter{max: c.maxOut}
	cmd.Stdout = w
	lowPriorityPre(cmd)
	if err := cmd.Start(); err != nil {
		return cmdResult{}, fmt.Errorf("iniciando %s: %w", c.path, err)
	}
	lowPriorityPost(cmd)
	err := cmd.Wait()
	if ctx.Err() != nil {
		return cmdResult{}, fmt.Errorf("%s excedio el tiempo o se cancelo: %w", c.path, ctx.Err())
	}
	res := cmdResult{out: w.buf, truncated: w.truncated}
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return cmdResult{}, fmt.Errorf("ejecutando %s: %w", c.path, err)
		}
		res.code = ee.ExitCode()
	}
	return res, nil
}
