import { OctagonAlertIcon, TriangleAlertIcon, InfoIcon } from 'lucide-react'
import { Link } from 'react-router'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusTile, TILE_LINK } from '@/components/ui/status-tile'
import { useAlertSummary } from '../hooks/useAlerts'
import type { AlertSeverity } from '../types/alert.types'
import { openBySeverity, openCount } from '../utils/summary'

const TILES: { label: string; severity: AlertSeverity; Icon: typeof OctagonAlertIcon; icon: string }[] = [
  { label: 'Críticas', severity: 'critical', Icon: OctagonAlertIcon, icon: 'text-danger' },
  { label: 'Advertencias', severity: 'warn', Icon: TriangleAlertIcon, icon: 'text-warn' },
  { label: 'Info', severity: 'info', Icon: InfoIcon, icon: 'text-info' },
]

/**
 * Bloque "Alertas abiertas · N" del resumen: una tarjeta por severidad (apiladas, 3 filas como los bloques de hosts y
 * dispositivos); cada una enlaza a la lista de alertas abiertas de esa severidad.
 */
export function AlertsBlock() {
  const { data, error } = useAlertSummary()
  if (!data && error) return null // sin datos: mejor omitir el bloque que mostrar esqueletos eternos
  return (
    <section aria-labelledby="summary-alerts" className="flex flex-col gap-3">
      <h2 id="summary-alerts" className="text-sm font-semibold">
        Alertas abiertas{data ? ` · ${openCount(data).toLocaleString('es-PE')}` : ''}
      </h2>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3 lg:grid-cols-1">
        {TILES.map((t) =>
          data ? (
            <Link key={t.severity} to={`/alertas?state=open&severity=${t.severity}`} className={TILE_LINK}>
              <StatusTile
                label={t.label}
                value={openBySeverity(data, t.severity)}
                Icon={t.Icon}
                iconClassName={t.icon}
                alert={t.severity === 'critical' && openBySeverity(data, t.severity) > 0}
              />
            </Link>
          ) : (
            <Skeleton key={t.severity} className="h-24" />
          ),
        )}
      </div>
    </section>
  )
}
