export type HostStatus = 'pending' | 'up' | 'down' | 'offline' | 'disabled'

export interface GroupRef {
  id: string
  name: string
}

/** Peor montaje del host (lista). */
export interface DiskUsage {
  pct: number
  mount: string
}

export interface Battery {
  pct: number
  charging: boolean | null
}

/** Fila de GET /hosts. */
export interface Host {
  id: string
  hostname: string
  status: HostStatus
  status_changed_at: string
  last_seen_at: string | null
  group: GroupRef | null
  os: string
  os_name?: string | null
  os_version?: string | null
  ip: string | null
  cpu_pct: number | null
  mem_pct: number | null
  disk: DiskUsage | null
  boot_time: string | null
  battery: Battery | null
}

export interface HostListResponse {
  items: Host[]
  next_cursor: string | null
  total: number
}

/** Comandos de instalacion que devuelve el servidor (si ya los soporta). */
export interface EnrollCommands {
  windows?: string
  linux?: string
}

export interface EnrollToken {
  enroll_token: string
  expires_at: string
  commands?: EnrollCommands
}

// --- Detalle de host y metricas (GET /hosts/{id} y /hosts/{id}/metrics) ---

export type MetricRange = '1h' | '6h' | '24h' | '7d' | '30d' | '90d' | '1y'

export interface HostDetail {
  id: string
  hostname: string
  os: string
  arch: string
  status: HostStatus
  last_seen_at: string | null
  status_changed_at: string
  created_at: string
  group: GroupRef | null
}

/** Inventario; todo null si el agente no lo mando. */
export interface HostInfo {
  boot_time: string | null
  cpu_model: string | null
  cpu_cores: number | null
  cpu_threads: number | null
  os_name: string | null
  os_version: string | null
  kernel_version: string | null
  agent_version: string | null
  ips: string[]
  mem_total_mb: number | null
}

export interface HostDisk {
  mount: string
  used_mb: number
  total_mb: number
  ts: string
}

/** Una muestra de la serie. `net_*_bps` (bytes/s) es null cuando no hay tasa calculable (p. ej. la primera muestra). */
export interface MetricSample {
  ts: string
  cpu_pct: number
  mem_used_mb: number
  mem_total_mb: number
  net_rx_bps: number | null
  net_tx_bps: number | null
  swap_used_mb: number | null
  swap_total_mb: number | null
  load1: number | null
  load5: number | null
  load15: number | null
  battery_pct: number | null
}

/** Ultima muestra: lo de la serie mas swap, carga y bateria (todo puede ser null). */
export interface HostLatest {
  ts: string
  cpu_pct: number | null
  mem_used_mb: number | null
  mem_total_mb: number | null
  net_rx_bps: number | null
  net_tx_bps: number | null
  swap_used_mb: number | null
  swap_total_mb: number | null
  load1: number | null
  load5: number | null
  load15: number | null
  battery_pct: number | null
  battery_charging: boolean | null
}

export interface HostDetailResponse {
  host: HostDetail
  info: HostInfo
  latest: HostLatest | null
  disks: HostDisk[]
}

export interface HostMetrics {
  range: MetricRange
  step_seconds: number
  points: MetricSample[]
}

// --- Grupo en mutaciones ---

export type BulkRequest =
  | { ids: string[]; action: 'set_group'; group_id: string | null }
  | { ids: string[]; action: 'disable' }

