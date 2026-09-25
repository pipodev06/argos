//go:build windows

package inventory

import "testing"

// TestSystemServicesReal usa el SCM real sin elevar: con derechos minimos debe resolver casi todo.
func TestSystemServicesReal(t *testing.T) {
	svcs, err := systemServices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(svcs) == 0 {
		t.Fatal("ningun servicio")
	}
	known := 0
	for _, s := range svcs {
		if s.Start != "other" {
			known++
		}
		switch s.State {
		case "running", "stopped", "paused", "other":
		default:
			t.Errorf("estado invalido %q", s.State)
		}
	}
	if known < len(svcs)/2 {
		t.Errorf("solo %d de %d con tipo de inicio: se abrio con derechos de mas?", known, len(svcs))
	}
}
