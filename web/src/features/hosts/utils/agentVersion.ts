/** Versión del agente en la tabla: vacía o `dev` (compilado sin versión) se muestran como "—". */
export function formatAgentVersion(version: string | null | undefined): string {
  const v = version?.trim()
  return !v || v === 'dev' ? '—' : v
}
