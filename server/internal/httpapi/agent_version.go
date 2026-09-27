package httpapi

import (
	"strconv"
	"strings"

	"argos/server/internal/store"
)

// devAgentVersion es la version de un agente compilado sin -ldflags: nunca cuenta como desactualizado.
const devAgentVersion = "dev"

// latestAgentVersion es la version del catalogo de binarios (ARGOS_AGENT_DIST_DIR); nil si no hay catalogo.
func (a *API) latestAgentVersion() *string {
	if a.opts.Dist == nil {
		return nil
	}
	v := a.opts.Dist.Version
	return &v
}

// isAgentOutdated dice si la version informada por un host esta desactualizada respecto del catalogo. Sin catalogo
// o sin version informada (dispositivos, agentes viejos) es false.
func (a *API) isAgentOutdated(version *string) bool {
	latest := a.latestAgentVersion()
	return latest != nil && version != nil && agentOutdated(*version, *latest)
}

// agentOutdated dice si un agente con la version dada esta desactualizado respecto de latest. "dev" y la version
// vacia no lo estan. Con las dos versiones numericas (1.2.3, con "v" y sufijo -pre o +build opcionales) esta
// desactualizado solo si es MENOR (un agente por delante del catalogo no lo esta); si alguna no se puede leer, si
// difiere de latest. Nunca falla por una version rara.
func agentOutdated(version, latest string) bool {
	version, latest = strings.TrimSpace(version), strings.TrimSpace(latest)
	if version == "" || version == devAgentVersion || version == latest {
		return false
	}
	vc, vpre, vok := parseSemver(version)
	lc, lpre, lok := parseSemver(latest)
	if !vok || !lok {
		return true
	}
	for i := range vc {
		if vc[i] != lc[i] {
			return vc[i] < lc[i]
		}
	}
	// Mismo numero: una version con sufijo pre-release (1.2.0-rc1) es anterior a la final (1.2.0).
	return vpre && !lpre
}

// parseSemver lee "v1.2.3-pre+build" como [1 2 3] (faltantes = 0) y si tenia sufijo pre-release. ok=false si el
// nucleo no es numerico o tiene mas de tres partes.
func parseSemver(s string) (core [3]int, pre, ok bool) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s, pre = s[:i], true
	}
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return core, false, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return core, false, false
		}
		core[i] = n
	}
	return core, pre, true
}

// outdatedAgentVersions devuelve, de las versiones que informan los hosts del ambito, las desactualizadas respecto
// de latest (resuelve ?agent=outdated de GET /hosts).
func outdatedAgentVersions(counts []store.AgentVersionCount, latest string) []string {
	out := []string{}
	for _, c := range counts {
		if agentOutdated(c.Version, latest) {
			out = append(out, c.Version)
		}
	}
	return out
}
