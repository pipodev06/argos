import type { LucideIcon as Icon } from "lucide-react"
import { OctagonAlertIcon, TriangleAlertIcon, CircleCheckIcon } from "lucide-react"
import { USAGE_CRIT_PCT, USAGE_WARN_PCT, clampPct, displayPct, usageLevel, type UsageLevel } from "@/lib/usage"

// El estado se dice siempre con icono + palabra; el color (relleno e icono) solo lo refuerza.
const LEVELS: Record<UsageLevel, { word: string; Icon: Icon; fill: string; icon: string }> = {
  ok: { word: "Normal", Icon: CircleCheckIcon, fill: "bg-ok", icon: "text-ok" },
  warn: { word: "Casi lleno", Icon: TriangleAlertIcon, fill: "bg-warn", icon: "text-warn" },
  critical: { word: "Lleno", Icon: OctagonAlertIcon, fill: "bg-danger", icon: "text-danger" },
}

interface UsageBarProps {
  /** Porcentaje de uso (0-100). */
  value: number
  /** Nombre accesible del medidor ("Uso de disco C:"). */
  label: string
  /** Oculta el porcentaje y la palabra de estado (no recomendado: el estado no debe depender solo del color). */
  hideStatus?: boolean
}

/**
 * Barra de uso `h-2` con marcas de 1px en los umbrales globales (80 % y 90 %). `role="meter"` con
 * `aria-valuenow`/`aria-valuetext`; estado con icono + palabra (Normal / Casi lleno / Lleno).
 */
function UsageBar({ value, label, hideStatus = false }: UsageBarProps) {
  const pct = clampPct(value)
  const shown = displayPct(value)
  const { word, Icon: StatusIcon, fill, icon } = LEVELS[usageLevel(value)]

  return (
    <div className="flex items-center gap-3">
      <div
        role="meter"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={shown}
        aria-valuetext={`${shown} %, ${word}`}
        className="relative flex h-3 min-w-24 flex-1 items-center"
      >
        <div className="h-2 w-full overflow-hidden rounded-md bg-border">
          <div className={`h-full ${fill}`} style={{ width: `${pct}%` }} />
        </div>
        {/* marcas de umbral: 1px, sobresalen 2px de la pista para verse tambien sobre el relleno */}
        <span aria-hidden className="absolute top-0 h-3 w-px bg-text" style={{ left: `${USAGE_WARN_PCT}%` }} />
        <span aria-hidden className="absolute top-0 h-3 w-px bg-text" style={{ left: `${USAGE_CRIT_PCT}%` }} />
      </div>
      {!hideStatus && (
        <div className="flex shrink-0 items-center gap-1.5 text-xs">
          <span className="w-9 text-right tabular-nums">{shown} %</span>
          <StatusIcon className={`size-4 shrink-0 ${icon}`} strokeWidth={1.5} aria-hidden />
          <span>{word}</span>
        </div>
      )}
    </div>
  )
}

export { UsageBar }
