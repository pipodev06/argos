// Conversion entre el valor de <input type="datetime-local"> (hora local, sin zona) y RFC 3339 UTC (URL/API).

const pad = (n: number) => String(n).padStart(2, '0')

/** RFC 3339 -> "YYYY-MM-DDTHH:mm" en hora local ('' si no es valido). */
export function toLocalInput(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** "YYYY-MM-DDTHH:mm" (local) -> RFC 3339 UTC sin milisegundos ('' si no es valido). */
export function fromLocalInput(local: string): string {
  const d = new Date(local)
  if (Number.isNaN(d.getTime())) return ''
  return d.toISOString().replace(/\.\d{3}Z$/, 'Z')
}

/** Resumen corto en hora local: "24/09/2026 10:30". */
export function formatDateTimeSummary(iso: string): string {
  const d = new Date(iso)
  return `${pad(d.getDate())}/${pad(d.getMonth() + 1)}/${d.getFullYear()} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}
