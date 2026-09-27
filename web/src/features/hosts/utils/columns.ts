export type OptionalColumn = 'ip' | 'uptime' | 'battery' | 'physical_disk'

export const OPTIONAL_COLUMNS: readonly { id: OptionalColumn; label: string }[] = [
  { id: 'ip', label: 'IP' },
  { id: 'uptime', label: 'Uptime' },
  { id: 'battery', label: 'Batería' },
  { id: 'physical_disk', label: 'Disco físico' },
]
