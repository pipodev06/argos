import type { ExpectedPort } from '../types/alert.types'

const MAX_EXPECTED_PORTS = 100

/** Texto "tcp/22, udp/53" (comas, espacios o saltos de línea) a lista validada y normalizada. */
export function parseExpectedPorts(raw: string): { ports: ExpectedPort[] } | { error: string } {
  const tokens = raw.split(/[\s,;]+/).filter((t) => t !== '')
  const ports: ExpectedPort[] = []
  const seen = new Set<string>()
  for (const token of tokens) {
    const m = /^(tcp|udp)\/(\d{1,5})$/i.exec(token)
    if (!m) return { error: `«${token}» no es válido: usa el formato tcp/22 o udp/53.` }
    const port = Number(m[2])
    if (port < 1 || port > 65535) return { error: `«${token}»: el puerto debe estar entre 1 y 65535.` }
    const proto = m[1].toLowerCase() as ExpectedPort['proto']
    const key = `${proto}/${port}`
    if (seen.has(key)) return { error: `«${key}» está repetido.` }
    seen.add(key)
    ports.push({ proto, port })
  }
  if (ports.length > MAX_EXPECTED_PORTS) return { error: `Máximo ${MAX_EXPECTED_PORTS} puertos.` }
  return { ports }
}

export const formatExpectedPorts = (ports: readonly ExpectedPort[]): string => ports.map((p) => `${p.proto}/${p.port}`).join(', ')
