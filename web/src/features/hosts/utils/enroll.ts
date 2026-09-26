import type { EnrollCommands, EnrollToken } from '../types/host.types'

/** Minutos que faltan para `expiresAt` (redondeado hacia arriba); 0 = expiro. */
export function minutesLeft(expiresAt: string, now: number): number {
  const ms = new Date(expiresAt).getTime() - now
  return ms <= 0 ? 0 : Math.ceil(ms / 60_000)
}

/**
 * Comandos por SO. Usa los que devuelve el servidor (`commands`); si aun no vienen, construye el del agente
 * (`argos-agent enroll --server <origen> --token-stdin`) en el cliente, con el token por stdin.
 */
export function enrollCommands(token: EnrollToken, origin: string): Required<EnrollCommands> {
  const quoted = `'${token.enroll_token}'`
  return {
    windows:
      token.commands?.windows ??
      `${quoted} | .\\argos-agent.exe enroll --server ${origin} --token-stdin`,
    linux:
      token.commands?.linux ??
      `echo ${quoted} | argos-agent enroll --server ${origin} --token-stdin`,
  }
}

/** Opciones del agente que se añaden como flags al comando de instalación. */
export interface AgentOptions {
  /** Inventario de puertos, servicios y procesos. */
  inventory: boolean
  /** Parches del sistema. */
  updates: boolean
  probe: boolean
  /** Redes CIDR separadas por comas; solo cuenta si `probe`. */
  probeNets: string
  privileged: boolean
  /** Solo válido si el servidor usa http; el llamador ya lo condiciona. */
  insecure: boolean
}

export const DEFAULT_AGENT_OPTIONS: AgentOptions = {
  inventory: true,
  updates: true,
  probe: false,
  probeNets: '',
  privileged: false,
  insecure: false,
}

const MAX_NETS = 32
const IPV4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/
const HEX_GROUP = /^[0-9a-fA-F]{1,4}$/

function isIPv4(addr: string): boolean {
  const m = IPV4.exec(addr)
  return m !== null && m.slice(1).every((o) => Number(o) <= 255)
}

function isIPv6(addr: string): boolean {
  if (addr.includes(':::')) return false
  const halves = addr.split('::')
  if (halves.length > 2) return false
  const groups = (h: string) => (h === '' ? [] : h.split(':'))
  const all = halves.flatMap(groups)
  if (!all.every((g) => HEX_GROUP.test(g))) return false
  return halves.length === 2 ? all.length <= 7 : all.length === 8
}

/** CIDR IPv4/IPv6 estricto; rechaza prefijo /0 (0.0.0.0/0 y ::/0 = toda la red). */
export function isValidCidr(cidr: string): boolean {
  const parts = cidr.split('/')
  if (parts.length !== 2 || !/^\d{1,3}$/.test(parts[1])) return false
  const prefix = Number(parts[1])
  if (prefix < 1) return false
  if (parts[0].includes(':')) return prefix <= 128 && isIPv6(parts[0])
  return prefix <= 32 && isIPv4(parts[0])
}

export type NetsResult = { ok: true; nets: string[] } | { ok: false; error: string }

/** Valida la lista separada por comas: al menos una red, todas válidas, sin `/0`, sin repetidas. */
export function parseNets(raw: string): NetsResult {
  const nets = raw.split(',').map((n) => n.trim()).filter((n) => n !== '')
  if (nets.length === 0) return { ok: false, error: 'Indica al menos una red, por ejemplo 192.168.1.0/24.' }
  if (nets.length > MAX_NETS) return { ok: false, error: `Máximo ${MAX_NETS} redes.` }
  const bad = nets.find((n) => !isValidCidr(n))
  if (bad !== undefined) {
    return {
      ok: false,
      error: /\/0$/.test(bad) ? `${bad}: no se permite abrir todo el rango (/0).` : `"${bad.slice(0, 45)}" no es una red CIDR válida.`,
    }
  }
  return { ok: true, nets: [...new Set(nets)] }
}

export type InstallCommand = { ok: true; command: string } | { ok: false; error: string }

/**
 * Añade los flags de `opts` al comando del servidor. Linux/macOS: al final (` --inventory a,b ...`).
 * Windows: antes de la comilla que cierra el `-Command` (` -Inventory 'a,b' ...`). Solo se escriben valores de
 * listas fijas o CIDR ya validados: nunca texto libre. Sin opciones el comando queda intacto; una red inválida
 * devuelve error (bloquea la copia). Un comando de Windows que no termina en `"` se deja intacto.
 */
export function applyAgentOptions(command: string, os: 'linux' | 'windows', opts: AgentOptions): InstallCommand {
  const inv = [...(opts.inventory ? ['ports', 'services', 'processes'] : []), ...(opts.updates ? ['updates'] : [])].join(',')
  let nets = ''
  if (opts.probe) {
    const r = parseNets(opts.probeNets)
    if (!r.ok) return r
    nets = r.nets.join(',')
  }
  const win = os === 'windows'
  const flags = [
    inv && (win ? `-Inventory '${inv}'` : `--inventory ${inv}`),
    nets && (win ? `-ProbeAllow '${nets}'` : `--probe-allow ${nets}`),
    opts.privileged && (win ? '-Privileged' : '--privileged'),
    opts.insecure && (win ? '-Insecure' : '--insecure'),
  ].filter((f): f is string => typeof f === 'string' && f !== '')
  if (flags.length === 0) return { ok: true, command }
  const extra = ` ${flags.join(' ')}`
  if (!win) return { ok: true, command: command + extra }
  if (!command.endsWith('"')) return { ok: true, command }
  return { ok: true, command: command.slice(0, -1) + extra + '"' }
}

/** `http://` = sin TLS: solo entonces se ofrece `--insecure`. */
export const isHttpOrigin = (origin: string): boolean => origin.startsWith('http://')
