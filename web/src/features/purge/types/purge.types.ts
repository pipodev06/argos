export interface DeletedRows {
  metrics: number
  metrics_hourly: number
  metrics_chunks: number
  disk_samples: number
  service_checks: number
}

/** POST /groups/{id}/purge. */
export interface GroupPurgeResult {
  agents: number
  devices: number
  deleted_rows: DeletedRows
  failed_host_ids: string[]
  rules: number
  windows: number
  checks: number
  enroll_tokens: number
  channels: number
  snmp_profiles: number
  users_disabled: number
  users_scope_reduced: number
  group_deleted: boolean
  /** Datos de la constancia. */
  group_name: string
  /** RFC3339 UTC. */
  purged_at: string
  performed_by: string
}

export interface GroupPurgeOptions {
  delete_group: boolean
  disable_users: boolean
}
