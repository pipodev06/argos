package config

import "testing"

func TestLoadProbeAllow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		env       map[string]string
		wantErr   bool
		wantOn    bool
		wantMaxCk int
	}{
		{"vacio = desactivado", nil, false, false, 20},
		{"allow valido", map[string]string{"ARGOS_PROBE_ALLOW": "192.168.1.0/24"}, false, true, 20},
		{"mascara muy grande", map[string]string{"ARGOS_PROBE_ALLOW": "10.0.0.0/16"}, true, false, 0},
		{"loopback", map[string]string{"ARGOS_PROBE_ALLOW": "127.0.0.1"}, true, false, 0},
		{"sobre el tope", map[string]string{"ARGOS_PROBE_ALLOW": "10.0.0.0/24,10.0.1.0/24"}, true, false, 0},
		{"tope subido", map[string]string{"ARGOS_PROBE_ALLOW": "10.0.0.0/24,10.0.1.0/24", "ARGOS_PROBE_MAX_TARGETS": "512"}, false, true, 20},
		{"tope invalido", map[string]string{"ARGOS_PROBE_MAX_TARGETS": "abc"}, true, false, 0},
		{"max checks subido", map[string]string{"ARGOS_PROBE_MAX_CHECKS": "300"}, false, false, 300},
		{"max checks bajo el defecto", map[string]string{"ARGOS_PROBE_MAX_CHECKS": "5"}, true, false, 0},
		{"max checks absurdo", map[string]string{"ARGOS_PROBE_MAX_CHECKS": "100000"}, true, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := Load(func(k string) string { return tt.env[k] }, func() (string, error) { return t.TempDir(), nil })
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got := cfg.ProbeAllow.Enabled(); got != tt.wantOn {
				t.Errorf("Enabled = %v, want %v", got, tt.wantOn)
			}
			if cfg.ProbeMaxChecks != tt.wantMaxCk {
				t.Errorf("ProbeMaxChecks = %d, want %d", cfg.ProbeMaxChecks, tt.wantMaxCk)
			}
		})
	}
}
