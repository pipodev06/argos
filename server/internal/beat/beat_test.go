package beat

import (
	"errors"
	"testing"
	"time"
)

func TestBeat(t *testing.T) {
	t.Parallel()
	var nilBeat *Beat
	nilBeat.OK(time.Now()) // no debe entrar en panico
	nilBeat.Fail(time.Now(), errors.New("x"))
	if got := nilBeat.Snapshot(); got != (State{}) {
		t.Errorf("Snapshot de nil got %+v, want vacio", got)
	}

	var b Beat
	t0 := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	b.OK(t0)
	b.Fail(t0.Add(time.Minute), errors.New("boom"))
	got := b.Snapshot()
	if !got.LastOK.Equal(t0) || !got.LastFail.Equal(t0.Add(time.Minute)) || got.LastErr != "boom" {
		t.Errorf("Snapshot got %+v", got)
	}
}
