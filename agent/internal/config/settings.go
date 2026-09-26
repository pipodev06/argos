package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"argos/agent/internal/checks"
	"argos/agent/internal/inventory"
)

const (
	// SettingsFileName es la config local que `install` deja junto a agent.json.
	SettingsFileName = "settings.json"
	maxSettingsBytes = 64 << 10
)

// Settings es la config persistente que `argos-agent install` guarda en el directorio de estado.
// Es solo LOCAL: la decide quien instala en el host, jamas el servidor. Cada campo equivale a una
// variable de entorno (que gana si esta definida) y se valida con las mismas reglas que ella.
type Settings struct {
	// Inventory son los modulos de inventario (ARGOS_INVENTORY_MODULES), ya normalizados.
	Inventory string `json:"inventory,omitempty"`
	// ProbeAllow es la allowlist del colector ping (ARGOS_PROBE_ALLOW).
	ProbeAllow string `json:"probe_allow,omitempty"`
	// Insecure permite http hacia un servidor remoto (ARGOS_ALLOW_INSECURE=1).
	Insecure bool `json:"insecure,omitempty"`
}

// Normalize valida los campos con los mismos parsers que las variables de entorno y deja
// Inventory canonico (sin repetidos, "all" expandido). ProbeAllow rechaza redes mas grandes que /24
// (incluidas 0.0.0.0/0 y ::/0) y rangos siempre bloqueados.
func (s Settings) Normalize() (Settings, error) {
	mods, err := inventory.ParseModules(s.Inventory)
	if err != nil {
		return Settings{}, fmt.Errorf("--inventory invalido: %w", err)
	}
	s.Inventory = strings.Join(mods, ",")
	s.ProbeAllow = strings.TrimSpace(s.ProbeAllow)
	if _, err := checks.ParseProbePolicy(s.ProbeAllow, checks.DefaultProbeMaxTargets); err != nil {
		return Settings{}, fmt.Errorf("--probe-allow invalido (IPs o CIDRs /24 o mas chicos separados por comas): %w", err)
	}
	return s, nil
}

// LoadSettings lee settings.json de dir. Un archivo ausente es la config vacia; uno ilegible
// falla con un mensaje claro (el agente no debe arrancar con una allowlist que no entiende).
func LoadSettings(dir string) (Settings, error) {
	path := filepath.Join(dir, SettingsFileName)
	f, err := os.Open(path) //nolint:gosec // ruta bajo el directorio de estado
	if errors.Is(err, os.ErrNotExist) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("abriendo %s: %w", path, err)
	}
	defer func() { _ = f.Close() }() // solo lectura
	var s Settings
	dec := json.NewDecoder(io.LimitReader(f, maxSettingsBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Settings{}, fmt.Errorf("%s ilegible (reinstala con `argos-agent install` o borralo): %w", path, err)
	}
	return s, nil
}

// SettingsWriter guarda archivos en el estado del agente (lo implementa *state.Store).
type SettingsWriter interface {
	WriteFile(name string, data []byte) error
}

// SaveSettings escribe settings.json (atomico, con los permisos restrictivos del estado).
func SaveSettings(w SettingsWriter, s Settings) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("serializando la config: %w", err)
	}
	if err := w.WriteFile(SettingsFileName, data); err != nil {
		return fmt.Errorf("guardando %s: %w", SettingsFileName, err)
	}
	return nil
}

// overlay devuelve un getenv que, para las variables que `install` puede fijar, cae al archivo
// cuando la variable de entorno esta vacia. Precedencia: entorno > archivo > defecto.
func (s Settings) overlay(getenv func(string) string) func(string) string {
	return func(key string) string {
		if v := getenv(key); strings.TrimSpace(v) != "" {
			return v
		}
		switch key {
		case "ARGOS_INVENTORY_MODULES":
			return s.Inventory
		case "ARGOS_PROBE_ALLOW":
			return s.ProbeAllow
		case "ARGOS_ALLOW_INSECURE":
			if s.Insecure {
				return "1"
			}
		}
		return getenv(key)
	}
}
