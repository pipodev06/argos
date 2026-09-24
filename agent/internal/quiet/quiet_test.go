package quiet

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestTrackerLogsOnlyStateChanges(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tr := New(slog.New(slog.NewJSONHandler(&buf, nil)))
	ctx := context.Background()

	tr.OK(ctx, "bateria") // sana desde el inicio: nada
	for range 5 {
		tr.Fail(ctx, "bateria", errors.New("sin acceso"))
	}
	if n := strings.Count(buf.String(), "fallida"); n != 1 {
		t.Errorf("fallos registrados got %d, want 1 (sin spam):\n%s", n, buf.String())
	}
	tr.OK(ctx, "bateria")
	tr.OK(ctx, "bateria")
	if n := strings.Count(buf.String(), "recuperada"); n != 1 {
		t.Errorf("recuperaciones got %d, want 1:\n%s", n, buf.String())
	}
	tr.Fail(ctx, "bateria", errors.New("otra vez"))
	if n := strings.Count(buf.String(), "fallida"); n != 2 {
		t.Errorf("un fallo tras recuperarse debe registrarse de nuevo, got %d:\n%s", n, buf.String())
	}
}

func TestNilLoggerIsSilent(t *testing.T) {
	t.Parallel()
	New(nil).Fail(context.Background(), "x", errors.New("e")) // no debe entrar en panico
}
