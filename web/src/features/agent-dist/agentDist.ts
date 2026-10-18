/** Manifiesto publico de las descargas del agente (GET /dl/latest/manifest.json; lo arma build-agent.sh). */
export interface AgentDistFile {
  name: string
  os: string
  arch: string
  sha256: string
  size: number
}

export interface AgentDistManifest {
  version: string
  files: AgentDistFile[]
}

/** El manifiesto vive fuera de /api (lo bajan tambien los instaladores, sin sesion). 404 = sin binarios (lista vacia). */
export async function getAgentManifest(signal?: AbortSignal): Promise<AgentDistManifest> {
  const res = await fetch('/dl/latest/manifest.json', { signal, credentials: 'same-origin' })
  if (res.status === 404) return { version: '', files: [] }
  if (!res.ok) throw new Error(`No se pudo leer el listado de descargas (HTTP ${res.status}).`)
  return (await res.json()) as AgentDistManifest
}

export const OS_LABELS: Record<string, string> = { windows: 'Windows', linux: 'Linux', darwin: 'macOS' }
const ARCH_LABELS: Record<string, string> = { amd64: 'x64 (Intel/AMD)', arm64: 'ARM64' }

/** Arquitectura legible; en macOS, Intel o Apple Silicon. */
export function archLabel(os: string, arch: string): string {
  if (os === 'darwin') return arch === 'arm64' ? 'Apple Silicon (M1 o posterior)' : 'Intel'
  return ARCH_LABELS[arch] ?? arch
}

/** Tipo de archivo por extension: el instalador .deb, el ejecutable o un comprimido. */
export function fileKind(name: string): string {
  if (name.endsWith('.deb')) return 'Paquete .deb'
  if (name.endsWith('.exe')) return 'Ejecutable .exe'
  if (name.endsWith('.tar.gz') || name.endsWith('.tgz')) return 'Comprimido .tar.gz'
  if (name.endsWith('.zip')) return 'Comprimido .zip'
  return 'Binario'
}

/** Orden de la tabla: Windows, Linux, macOS; dentro, x64 antes que ARM y el paquete antes que el binario. */
export function sortFiles(files: readonly AgentDistFile[]): AgentDistFile[] {
  const osRank = (os: string) => ['windows', 'linux', 'darwin'].indexOf(os) + 1 || 9
  return [...files].sort(
    (a, b) => osRank(a.os) - osRank(b.os) || a.arch.localeCompare(b.arch) || Number(b.name.endsWith('.deb')) - Number(a.name.endsWith('.deb')),
  )
}
