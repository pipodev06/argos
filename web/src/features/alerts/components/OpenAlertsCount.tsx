import { useAlertSummary } from '../hooks/alertSummaryContext'
import { openCount } from '../utils/summary'

/** Contador de alertas abiertas (sin reconocer) junto al ítem del sidebar; no se muestra en 0 ni sin datos. */
export function OpenAlertsCount() {
  const { data } = useAlertSummary()
  const count = data ? openCount(data) : 0
  if (count === 0) return null
  return (
    <span
      className="ml-auto rounded-md bg-danger/10 px-1.5 text-xs font-medium tabular-nums text-text"
      aria-label={`${count} abiertas`}
    >
      {count > 999 ? '999+' : count}
    </span>
  )
}
