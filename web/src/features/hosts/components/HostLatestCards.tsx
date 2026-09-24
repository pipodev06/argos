import {
  IconAlertTriangle,
  IconArrowDown,
  IconArrowUp,
  IconBattery,
  IconBattery1,
  IconBattery2,
  IconBattery3,
  IconBattery4,
  IconBatteryCharging,
  IconClockPlay,
  IconCpu,
  IconGauge,
  IconNetwork,
  IconStack2,
} from '@tabler/icons-react'
import { StatCard, StatValue } from '@/components/ui/stat-card'
import type { HostInfo, HostLatest, HostStatus } from '../types/host.types'
import {
  formatBps,
  formatElapsed,
  formatFullDate,
  formatMemory,
  formatNumber,
  formatPercent,
} from '../utils/format'

interface HostLatestCardsProps {
  /** Ultima muestra; null = el host aun no reporto. */
  latest: HostLatest | null
  info: HostInfo | null
  status: HostStatus | null
  pending: boolean
}

const LOW_BATTERY_PCT = 20

function batteryIcon(pct: number, charging: boolean | null) {
  if (charging) return IconBatteryCharging
  if (pct >= 88) return IconBattery4
  if (pct >= 63) return IconBattery3
  if (pct >= 38) return IconBattery2
  if (pct >= 13) return IconBattery1
  return IconBattery
}

function ratio(used: number | null | undefined, total: number | null | undefined): number | null {
  return used != null && total != null && total > 0 ? (used / total) * 100 : null
}

// Estos valores tambien son la alternativa textual de las graficas.
export function HostLatestCards({ latest, info, status, pending }: HostLatestCardsProps) {
  const memPct = ratio(latest?.mem_used_mb, latest?.mem_total_mb)
  const swapPct = ratio(latest?.swap_used_mb, latest?.swap_total_mb)
  const noSwap = latest != null && (latest.swap_total_mb == null || latest.swap_total_mb === 0)
  const hasLoad = latest?.load1 != null
  const batteryPct = latest?.battery_pct ?? null
  const charging = latest?.battery_charging ?? null
  const BatteryIcon = batteryPct === null ? IconBattery : batteryIcon(batteryPct, charging)
  const lowBattery = batteryPct !== null && batteryPct < LOW_BATTERY_PCT && charging === false
  const boot = info?.boot_time ?? null

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <StatCard label="CPU" Icon={IconCpu}>
        <StatValue pending={pending}>{latest?.cpu_pct != null ? formatPercent(latest.cpu_pct) : null}</StatValue>
      </StatCard>

      <StatCard label="Memoria" Icon={IconStack2}>
        <StatValue pending={pending} detail={memPct === null ? null : formatPercent(memPct)}>
          {latest?.mem_used_mb != null && latest.mem_total_mb != null
            ? `${formatMemory(latest.mem_used_mb)} / ${formatMemory(latest.mem_total_mb)}`
            : null}
        </StatValue>
        {latest && (
          <p className="text-xs text-text-muted tabular-nums">
            {noSwap
              ? 'Sin swap'
              : latest.swap_used_mb != null && latest.swap_total_mb != null
                ? `Swap ${formatMemory(latest.swap_used_mb)} / ${formatMemory(latest.swap_total_mb)}${swapPct === null ? '' : ` · ${formatNumber(swapPct, 0)} %`}`
                : null}
          </p>
        )}
      </StatCard>

      <StatCard label="Red" Icon={IconNetwork}>
        <StatValue size="md" pending={pending}>
          {latest?.net_rx_bps == null ? null : (
            <span className="inline-flex items-center gap-1">
              <IconArrowDown className="size-4" stroke={1.5} aria-hidden />
              <span className="sr-only">Bajada</span>
              {formatBps(latest.net_rx_bps)}
            </span>
          )}
        </StatValue>
        <StatValue size="md" pending={pending}>
          {latest?.net_tx_bps == null ? null : (
            <span className="inline-flex items-center gap-1">
              <IconArrowUp className="size-4" stroke={1.5} aria-hidden />
              <span className="sr-only">Subida</span>
              {formatBps(latest.net_tx_bps)}
            </span>
          )}
        </StatValue>
      </StatCard>

      <StatCard label="Carga" Icon={IconGauge}>
        {hasLoad ? (
          <StatValue detail="1 / 5 / 15 min">
            {[latest.load1, latest.load5, latest.load15]
              .map((v) => (v == null ? '—' : formatNumber(v, 2)))
              .join(' / ')}
          </StatValue>
        ) : (
          <StatValue pending={pending}>{null}</StatValue>
        )}
        {!pending && !hasLoad && <p className="text-xs text-text-muted">No disponible en este sistema.</p>}
      </StatCard>

      <StatCard label="En marcha" Icon={IconClockPlay}>
        <StatValue pending={pending}>{boot && status === 'up' ? formatElapsed(boot) : null}</StatValue>
        {boot && status === 'up' && (
          <p className="text-xs text-text-muted tabular-nums">Arrancó {formatFullDate(boot)}</p>
        )}
      </StatCard>

      {batteryPct !== null && (
        <StatCard label="Batería" Icon={BatteryIcon}>
          <StatValue pending={pending} detail={charging === null ? null : charging ? (batteryPct >= 100 ? 'Completa' : 'Cargando') : 'Descargando'}>
            {`${batteryPct} %`}
          </StatValue>
          {lowBattery && (
            <p className="flex items-center gap-1 text-xs">
              <IconAlertTriangle className="size-3.5 shrink-0 text-warn" stroke={1.5} aria-hidden />
              Batería baja
            </p>
          )}
        </StatCard>
      )}
    </div>
  )
}
