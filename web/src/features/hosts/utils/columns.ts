export type OptionalColumn = 'ip' | 'tags' | 'uptime' | 'battery'

export const OPTIONAL_COLUMNS: readonly { id: OptionalColumn; label: string }[] = [
  { id: 'ip', label: 'IP' },
  { id: 'tags', label: 'Etiquetas' },
  { id: 'uptime', label: 'Uptime' },
  { id: 'battery', label: 'Batería' },
]
