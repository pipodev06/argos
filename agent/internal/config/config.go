// Package config carga y valida la configuracion del agente desde variables de entorno.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"argos/agent/internal/checks"
	"argos/agent/internal/inventory"
	"argos/agent/internal/state"
)

const (
	defaultInterval     = 30 * time.Second
	minInterval         = 5 * time.Second
	defaultDiskInterval = 300 * time.Second
	minDiskInterval     = 60 * time.Second

	// stateDirName es el subdirectorio de os.UserConfigDir() donde vive el estado.
	stateDirName = "argos-agent"
)

// Config es la configuracion ya validada del agente. Todo es opcional a este
// nivel: cada subcomando exige lo que necesita (ver ValidateServerURL y state.Resolve).
type Config struct {
	// ServerURL es la URL base del servidor (sin path), p. ej. https://argos.example.com.
	// Vacia si no hay variable: el subcomando la completa desde flag o archivo de estado.
	ServerURL string
	// Token es el override de agent token (ARGOS_AGENT_TOKEN). Secreto: nunca se loguea ni va en el body.
	Token string
	// EnrollToken es el token de un solo uso pa enrolar (ARGOS_ENROLL_TOKEN). Secreto.
	EnrollToken string
	// Interval es cada cuanto se reportan metricas.
	Interval time.Duration
	// DiskInterval es cada cuanto se reporta el uso de discos (ademas del primer reporte).
	DiskInterval time.Duration
	// StateDir es el directorio del archivo de estado con el agent token.
	StateDir string
	// CAFile es una CA privada (PEM) pa validar el certificado del servidor; vacio = CAs del sistema.
	// Si ARGOS_CA_FILE no esta definida y existe <StateDir>/ca.pem, se usa ese.
	CAFile string
	// AllowInsecure permite http hacia hosts que no son loopback (ARGOS_ALLOW_INSECURE=1).
	AllowInsecure bool
	// CheckAllow es la politica local de destinos de los chequeos (ARGOS_CHECK_ALLOW). Vacia = defecto:
	// solo loopback y las IPs propias del host. El servidor no puede ampliarla.
	CheckAllow *checks.Policy
	// InventoryModules son los modulos de inventario activados localmente (ARGOS_INVENTORY_MODULES:
	// ports, services, processes). Vacio = apagado (defecto). El servidor no puede activarlos.
	InventoryModules []string
}

// Load lee la configuracion con getenv (os.Getenv en produccion). defaultStateDir
// da el directorio de estado cuando ARGOS_STATE_DIR no esta definida (el del
// usuario o el del servicio, lo decide el llamador). Falla con un mensaje claro si
// algo es invalido.
func Load(getenv func(string) string, defaultStateDir func() (string, error)) (Config, error) {
	cfg := Config{
		ServerURL:    strings.TrimSpace(getenv("ARGOS_SERVER_URL")),
		Token:        strings.TrimSpace(getenv("ARGOS_AGENT_TOKEN")),
		EnrollToken:  strings.TrimSpace(getenv("ARGOS_ENROLL_TOKEN")),
		Interval:     defaultInterval,
		DiskInterval: defaultDiskInterval,
		StateDir:     getenv("ARGOS_STATE_DIR"),
		CAFile:       getenv("ARGOS_CA_FILE"),
	}

	if cfg.ServerURL != "" {
		if _, err := parseServerURL(cfg.ServerURL); err != nil {
			return Config{}, fmt.Errorf("ARGOS_SERVER_URL invalida: %w", err)
		}
	}
	if raw := getenv("ARGOS_INTERVAL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("ARGOS_INTERVAL invalida (ej. 30s): %w", err)
		}
		if d < minInterval {
			return Config{}, fmt.Errorf("ARGOS_INTERVAL minimo %s, se recibio %s", minInterval, d)
		}
		cfg.Interval = d
	}
	if raw := getenv("ARGOS_DISK_INTERVAL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("ARGOS_DISK_INTERVAL invalida (ej. 300s): %w", err)
		}
		if d < minDiskInterval {
			return Config{}, fmt.Errorf("ARGOS_DISK_INTERVAL minimo %s, se recibio %s", minDiskInterval, d)
		}
		cfg.DiskInterval = d
	}
	switch strings.ToLower(strings.TrimSpace(getenv("ARGOS_ALLOW_INSECURE"))) {
	case "", "0", "false":
	case "1", "true":
		cfg.AllowInsecure = true
	default:
		return Config{}, errors.New("ARGOS_ALLOW_INSECURE invalida: usa 1 o 0")
	}
	pol, err := checks.ParsePolicy(getenv("ARGOS_CHECK_ALLOW"))
	if err != nil {
		return Config{}, fmt.Errorf("ARGOS_CHECK_ALLOW invalida (hostnames, IPs o CIDRs separados por comas, con :puerto opcional): %w", err)
	}
	cfg.CheckAllow = pol
	mods, err := inventory.ParseModules(getenv("ARGOS_INVENTORY_MODULES"))
	if err != nil {
		return Config{}, fmt.Errorf("ARGOS_INVENTORY_MODULES invalida (lista separada por comas de ports, services, processes): %w", err)
	}
	cfg.InventoryModules = mods
	if cfg.StateDir == "" {
		dir, err := defaultStateDir()
		if err != nil {
			return Config{}, fmt.Errorf("no se pudo determinar el directorio de estado, define ARGOS_STATE_DIR: %w", err)
		}
		cfg.StateDir = dir
	}
	if cfg.CAFile == "" {
		if ca := filepath.Join(cfg.StateDir, state.CAFileName); fileExists(ca) {
			cfg.CAFile = ca
		}
	}
	return cfg, nil
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}

// UserStateDir es el directorio de estado por defecto de una ejecucion normal
// (no como servicio): <os.UserConfigDir()>/argos-agent.
func UserStateDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("directorio de configuracion del usuario: %w", err)
	}
	return filepath.Join(base, stateDirName), nil
}

// ErrInsecureTransport indica que se pidio http hacia un host remoto sin permitirlo.
var ErrInsecureTransport = errors.New("http hacia un host que no es loopback: el token viajaria sin cifrar")

// ValidateServerURL valida la URL base del servidor y la politica de transporte:
// https siempre; http solo hacia loopback, salvo allowInsecure. Con allowInsecure
// y http remoto, insecureRemote es true (el llamador debe advertirlo fuerte).
func ValidateServerURL(raw string, allowInsecure bool) (insecureRemote bool, err error) {
	u, err := parseServerURL(raw)
	if err != nil {
		return false, err
	}
	if u.Scheme != "http" || isLoopbackHost(u.Hostname()) {
		return false, nil
	}
	if !allowInsecure {
		return false, fmt.Errorf("%w: usa https o, bajo tu riesgo, ARGOS_ALLOW_INSECURE=1", ErrInsecureTransport)
	}
	return true, nil
}

func parseServerURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, errors.New("se espera http(s)://host[:puerto]")
	}
	if u.User != nil {
		return nil, errors.New("no se admiten credenciales dentro de la URL")
	}
	return u, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
