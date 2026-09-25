package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"slices"
	"time"

	"argos/server/internal/store"
)

// Limites del modulo updates (los mismos topes que aplica el agente).
const (
	maxUpdatesCount      = 1_000_000
	maxUpdatesCacheAgeS  = 10 * 365 * 24 * 3600
	maxUpdatesAdvisories = 200
	minUpdatesInstalled  = 1990
	updatesFutureSlack   = 48 * time.Hour
)

var (
	updatesAdvisoryRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,31}$`) // sin espacios ni caracteres de control
	validUpdatesSrc   = map[string]bool{"wua": true, "apt": true, "dnf": true, "yum": true, "zypper": true, "softwareupdate": true, "none": true}
)

// updatesItems es el objeto items del modulo updates. Todo campo ausente o null es "desconocido" (nunca 0).
type updatesItems struct {
	PendingTotal    *int64   `json:"pending_total"`
	PendingSecurity *int64   `json:"pending_security"`
	RebootRequired  *bool    `json:"reboot_required"`
	LastInstalled   *string  `json:"last_installed"`
	Source          string   `json:"source"`
	CacheAgeS       *int64   `json:"cache_age_s"`
	Advisories      []string `json:"advisories"`
}

// validateUpdates decodifica items (un OBJETO) con lista blanca estricta y valida rangos, largos y fechas. Los ids
// de aviso repetidos se descartan; superar el tope es 400 (el agente ya recorta a 200).
func validateUpdates(raw json.RawMessage, rev string, truncated bool, collectedAt, now time.Time) (store.UpdatesSnapshot, error) {
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return store.UpdatesSnapshot{}, badRequest("items debe ser un objeto")
	}
	var in updatesItems
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return store.UpdatesSnapshot{}, badRequest("items invalido")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return store.UpdatesSnapshot{}, badRequest("items invalido")
	}
	out := store.UpdatesSnapshot{Rev: rev, RebootRequired: in.RebootRequired, AdvisoriesTruncated: truncated, CollectedAt: collectedAt}
	var err error
	if out.PendingTotal, err = updatesCount("pending_total", in.PendingTotal); err != nil {
		return store.UpdatesSnapshot{}, err
	}
	if out.PendingSecurity, err = updatesCount("pending_security", in.PendingSecurity); err != nil {
		return store.UpdatesSnapshot{}, err
	}
	if out.PendingTotal != nil && out.PendingSecurity != nil && *out.PendingSecurity > *out.PendingTotal {
		return store.UpdatesSnapshot{}, badRequest("pending_security no puede superar pending_total")
	}
	if in.LastInstalled != nil {
		d, err := time.Parse(time.DateOnly, *in.LastInstalled)
		if err != nil || d.Year() < minUpdatesInstalled || d.After(now.Add(updatesFutureSlack)) {
			return store.UpdatesSnapshot{}, badRequest("last_installed invalido")
		}
		out.LastInstalled = &d
	}
	if !validUpdatesSrc[in.Source] {
		return store.UpdatesSnapshot{}, badRequest("source invalido")
	}
	out.Source = in.Source
	if in.CacheAgeS != nil {
		if *in.CacheAgeS < 0 || *in.CacheAgeS > maxUpdatesCacheAgeS {
			return store.UpdatesSnapshot{}, badRequest("cache_age_s fuera de rango")
		}
		out.CacheAgeS = in.CacheAgeS
	}
	if len(in.Advisories) > maxUpdatesAdvisories {
		return store.UpdatesSnapshot{}, badRequest("maximo %d advisories", maxUpdatesAdvisories)
	}
	for i, a := range in.Advisories {
		if !updatesAdvisoryRE.MatchString(a) {
			return store.UpdatesSnapshot{}, badRequest("advisories[%d] invalido", i)
		}
	}
	out.Advisories = slices.Compact(slices.Sorted(slices.Values(in.Advisories)))
	return out, nil
}

func updatesCount(field string, v *int64) (*int, error) {
	if v == nil {
		return nil, nil
	}
	if *v < 0 || *v > maxUpdatesCount {
		return nil, badRequest("%s fuera de rango", field)
	}
	n := int(*v)
	return &n, nil
}
