import type { AlertSeverity, AlertSummary } from '../types/alert.types'

/** Alertas abiertas (sin reconocer) de una severidad. */
export const openBySeverity = (s: AlertSummary, severity: AlertSeverity): number => s.by_severity[severity]?.open ?? 0

/** Alertas abiertas (sin reconocer) de todas las severidades. */
export const openCount = (s: AlertSummary): number =>
  openBySeverity(s, 'critical') + openBySeverity(s, 'warn') + openBySeverity(s, 'info')
