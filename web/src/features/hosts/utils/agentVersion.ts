/** Versión del agente en la tabla: vacía o `dev` (compilado sin versión) se muestran como "—". */
export function formatAgentVersion(version: string | null | undefined): string {
  const v = version?.trim()
  return !v || v === 'dev' ? '—' : v
}

function parseSemver(s: string): { core: number[]; pre: boolean } | null {
  let rest = s.replace(/^[vV]/, '')
  const plus = rest.indexOf('+')
  if (plus >= 0) rest = rest.slice(0, plus)
  let pre = false
  const dash = rest.indexOf('-')
  if (dash >= 0) {
    pre = true
    rest = rest.slice(0, dash)
  }
  const parts = rest.split('.')
  if (parts.length > 3 || parts.some((p) => !/^\d+$/.test(p))) return null
  const core = parts.map(Number)
  while (core.length < 3) core.push(0)
  return { core, pre }
}

/** Espejo de `agentOutdated` del servidor (agent_version.go): `dev`/vacía nunca; menor que la última, sí; ilegible y distinta, sí. */
export function isAgentOutdated(version: string, latest: string): boolean {
  const v = version.trim()
  const l = latest.trim()
  if (v === '' || v === 'dev' || v === l) return false
  const pv = parseSemver(v)
  const pl = parseSemver(l)
  if (!pv || !pl) return true
  for (let i = 0; i < 3; i++) {
    if (pv.core[i] !== pl.core[i]) return pv.core[i] < pl.core[i]
  }
  return pv.pre && !pl.pre
}

/** Hosts con agente desactualizado según `summary.agent_versions`; 0 sin catálogo (`latest` nulo). */
export function outdatedAgentCount(versions: { latest: string | null; counts: readonly { version: string; hosts: number }[] }): number {
  const { latest, counts } = versions
  if (latest === null) return 0
  return counts.reduce((n, c) => (isAgentOutdated(c.version, latest) ? n + c.hosts : n), 0)
}
