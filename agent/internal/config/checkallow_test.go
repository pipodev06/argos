package config

import "testing"

func TestLoadCheckAllow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"ausente = defecto", "", false},
		{"lista valida", "db.internal, 10.0.0.0/24, 10.0.0.5:22", false},
		{"entrada invalida", "no valido", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := Load(func(k string) string {
				if k == "ARGOS_CHECK_ALLOW" {
					return tt.raw
				}
				return ""
			}, stateDir)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && cfg.CheckAllow == nil {
				t.Error("CheckAllow nil: debe ser una politica (vacia = defecto)")
			}
		})
	}
}
