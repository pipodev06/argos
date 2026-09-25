export type OptionalColumn = 'ip' | 'uptime' | 'battery'

export const OPTIONAL_COLUMNS: readonly { id: OptionalColumn; label: string }[] = [
  { id: 'ip', label: 'IP' },
  { id: 'uptime', label: 'Uptime' },
  { id: 'battery', label: 'Batería' },
]
