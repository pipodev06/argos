import { IconAlertOctagon, IconAlertTriangle, IconBell, IconInfoCircle } from '@tabler/icons-react'
import { Link } from 'react-router'
import { Skeleton } from '@/components/ui/skeleton'
import { StatCard, StatValue } from '@/components/ui/stat-card'
import { useAlertSummary } from '../hooks/useAlerts'
import { openBySeverity, openCount } from '../utils/summary'

const TILE_LINK =
  'block rounded-md outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-bg'

/** Tarjeta "Alertas abiertas" del resumen: total y desglose por severidad; enlaza a la lista filtrada. */
export function AlertsTile() {
  const { data, error } = useAlertSummary()
  if (!data && error) return null // sin datos: mejor omitir la tarjeta que mostrar un esqueleto eterno
  if (!data) return <Skeleton className="h-24" />
  const total = openCount(data)
  const parts = [
    { label: 'críticas', value: openBySeverity(data, 'critical'), Icon: IconAlertOctagon, icon: 'text-danger' },
    { label: 'advertencias', value: openBySeverity(data, 'warn'), Icon: IconAlertTriangle, icon: 'text-warn' },
    { label: 'info', value: openBySeverity(data, 'info'), Icon: IconInfoCircle, icon: 'text-info' },
  ]
  return (
    <Link to="/alertas?state=open" className={TILE_LINK}>
      <StatCard label="Alertas abiertas" Icon={IconBell} alert={openBySeverity(data, 'critical') > 0}>
        <StatValue
          detail={
            <span className="flex flex-wrap gap-x-3">
              {parts.map(({ label, value, Icon, icon }) => (
                <span key={label} className="inline-flex items-center gap-1">
                  <Icon className={`size-3.5 shrink-0 ${icon}`} stroke={1.5} aria-hidden />
                  {value} {label}
                </span>
              ))}
            </span>
          }
        >
          {total.toLocaleString('es-PE')}
        </StatValue>
      </StatCard>
    </Link>
  )
}
