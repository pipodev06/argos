package inventory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// ModuleUpdates es el modulo de parches del SO (apagado por defecto). Solo LEE: nunca actualiza,
// instala ni refresca caches, y no envia titulos, URLs, descripciones ni rutas.
const ModuleUpdates = "updates"

const (
	maxAdvisories   = 200
	maxAdvisoryLen  = 32
	maxUpdatesCount = 1_000_000 // tope de cordura de un contador
	maxCacheAgeS    = 10 * 365 * 24 * 3600
)

// Valores de UpdatesConfig.WindowsOnline.
const (
	WindowsOnlineAuto   = "auto"   // cache local; en linea solo si la cache tiene mas de 24 h
	WindowsOnlineNever  = "never"  // solo cache local
	WindowsOnlineAlways = "always" // siempre en linea (contacta a Microsoft o al WSUS)
)

// Limites de la frecuencia de recoleccion del modulo (ARGOS_UPDATES_INTERVAL).
const (
	DefaultUpdatesInterval = 6 * time.Hour
	MinUpdatesInterval     = time.Hour
	MaxUpdatesInterval     = 24 * time.Hour
)

// UpdatesConfig es la config local del modulo; el servidor no puede cambiarla.
type UpdatesConfig struct {
	Interval      time.Duration // 0 = DefaultUpdatesInterval
	WindowsOnline string        // "" = auto
	// UseSudo (ARGOS_UPDATES_SUDO; lo fija el instalador con --privileged) ejecuta dnf/yum/zypper con
	// `sudo -n --`, solo los comandos de PrivilegedCommands. Solo aplica en Linux.
	UseSudo bool
}

// ParseWindowsOnline valida ARGOS_UPDATES_WINDOWS_ONLINE (vacio = auto).
func ParseWindowsOnline(raw string) (string, error) {
	switch v := strings.ToLower(strings.TrimSpace(raw)); v {
	case "", WindowsOnlineAuto:
		return WindowsOnlineAuto, nil
	case WindowsOnlineNever, WindowsOnlineAlways:
		return v, nil
	default:
		return "", fmt.Errorf("valor %q (validos: %s, %s, %s)", raw, WindowsOnlineAuto, WindowsOnlineNever, WindowsOnlineAlways)
	}
}

// Updates es la foto de parches. Cada campo es tri-estado: null = "desconocido" (no se pudo
// medir); NUNCA se rellena con 0/false lo que no se midio. Lista blanca de campos.
type Updates struct {
	PendingTotal    *int     `json:"pending_total"`
	PendingSecurity *int     `json:"pending_security"`
	RebootRequired  *bool    `json:"reboot_required"`
	LastInstalled   *string  `json:"last_installed"` // AAAA-MM-DD (UTC)
	Source          string   `json:"source"`         // wua | apt | dnf | yum | zypper | softwareupdate | none
	CacheAgeS       *int64   `json:"cache_age_s"`    // edad de los datos del gestor; null = no se sabe
	Advisories      []string `json:"advisories"`     // ids de aviso que el gestor ya reporta (KB, RHSA...)
}

var advisoryRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,31}$`)

// normalize deja u en el formato del protocolo: contadores acotados, fecha valida, avisos
// validados/unicos/ordenados y acotados. Devuelve true si se descartaron avisos por el tope.
func (u *Updates) normalize() bool {
	if u.PendingTotal != nil && (*u.PendingTotal < 0 || *u.PendingTotal > maxUpdatesCount) {
		u.PendingTotal = nil
	}
	if u.PendingSecurity != nil && (*u.PendingSecurity < 0 || *u.PendingSecurity > maxUpdatesCount) {
		u.PendingSecurity = nil
	}
	if u.PendingTotal != nil && u.PendingSecurity != nil && *u.PendingSecurity > *u.PendingTotal {
		// incoherente (p. ej. salida parcial): mas confiable el total; la seguridad se marca desconocida
		u.PendingSecurity = nil
	}
	if u.LastInstalled != nil {
		d, err := time.Parse(time.DateOnly, *u.LastInstalled)
		if err != nil || d.After(time.Now().Add(48*time.Hour)) || d.Year() < 1990 {
			u.LastInstalled = nil
		}
	}
	if u.CacheAgeS != nil && (*u.CacheAgeS < 0 || *u.CacheAgeS > maxCacheAgeS) {
		u.CacheAgeS = nil
	}
	u.Source = clean(u.Source, 16)
	if u.Source == "" {
		u.Source = "none"
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(u.Advisories))
	for _, a := range u.Advisories {
		if advisoryRe.MatchString(a) && !seen[a] {
			seen[a] = true
			ids = append(ids, a)
		}
	}
	slices.Sort(ids)
	truncated := len(ids) > maxAdvisories
	if truncated {
		ids = ids[:maxAdvisories]
	}
	u.Advisories = ids
	return truncated
}

// updatesLine es la forma canonica que entra al rev: sin cache_age_s (cambia sin que cambie el estado).
func updatesLine(u Updates) string {
	num := func(p *int) string {
		if p == nil {
			return "?"
		}
		return fmt.Sprint(*p)
	}
	reboot, last := "?", "?"
	if u.RebootRequired != nil {
		reboot = fmt.Sprint(*u.RebootRequired)
	}
	if u.LastInstalled != nil {
		last = *u.LastInstalled
	}
	return strings.Join([]string{ModuleUpdates, u.Source, num(u.PendingTotal), num(u.PendingSecurity), reboot, last, strings.Join(u.Advisories, ",")}, fieldSep)
}

// collectUpdates arma la foto de parches con la fuente inyectada.
func collectUpdates(ctx context.Context, src Sources) (snapshot, error) {
	u, err := src.Updates(ctx)
	if err != nil {
		return snapshot{}, err
	}
	truncated := u.normalize()
	sum := sha256.Sum256([]byte(updatesLine(u)))
	return snapshot{rev: hex.EncodeToString(sum[:]), items: u, truncated: truncated}, nil
}
