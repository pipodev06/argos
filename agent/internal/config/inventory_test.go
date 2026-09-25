package config

import (
	"reflect"
	"testing"
)

func TestLoadInventoryModules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw     string
		want    []string
		wantErr bool
	}{
		{raw: "", want: nil}, // apagado por defecto
		{raw: "ports, services", want: []string{"ports", "services"}},
		{raw: "ports,gpu", wantErr: true},
	}
	for _, tt := range tests {
		cfg, err := Load(func(k string) string {
			if k == "ARGOS_INVENTORY_MODULES" {
				return tt.raw
			}
			return ""
		}, stateDir)
		if (err != nil) != tt.wantErr || !reflect.DeepEqual(cfg.InventoryModules, tt.want) {
			t.Errorf("modules %q = %v, %v; want %v (err=%v)", tt.raw, cfg.InventoryModules, err, tt.want, tt.wantErr)
		}
	}
}
