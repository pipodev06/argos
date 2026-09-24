import * as React from "react"
import { cn } from "cn"
import * as RechartsPrimitive from "recharts"

// shadcn chart recortado a Slate. Se elimino `ChartStyle` (inyecta un <style>, que la CSP
// `default-src 'self'` bloquea) y el sistema de `config`: los colores se pasan como `var(--chart-N)`
// directo a cada serie. Sin sombras, radio unico 6px, texto solo `text` / `text-muted`.

function ChartContainer({
  className,
  children,
  ...props
}: React.ComponentProps<"div"> & {
  children: React.ComponentProps<typeof RechartsPrimitive.ResponsiveContainer>["children"]
}) {
  return (
    <div
      data-slot="chart"
      className={cn(
        "h-full w-full text-xs tabular-nums [&_.recharts-layer]:outline-hidden [&_.recharts-surface]:outline-hidden",
        className
      )}
      {...props}
    >
      <RechartsPrimitive.ResponsiveContainer initialDimension={{ width: 320, height: 200 }}>
        {children}
      </RechartsPrimitive.ResponsiveContainer>
    </div>
  )
}

const ChartTooltip = RechartsPrimitive.Tooltip

// Lo minimo que se lee de cada entrada del tooltip de Recharts (una por serie).
interface TooltipEntry {
  name?: string | number
  value?: unknown
  stroke?: string
  strokeDasharray?: string | number
}

interface ChartTooltipContentProps {
  // Los inyecta Recharts al clonar el elemento.
  active?: boolean
  payload?: readonly TooltipEntry[]
  label?: string | number
  labelFormatter: (label: string | number) => string
  valueFormatter: (value: number) => string
}

/** Muestra de trazo (color + patron) para leyenda y tooltip: nunca depende solo del color. */
function LineSwatch({ color, dash }: { color: string; dash?: string | number }) {
  return (
    <svg width="20" height="8" viewBox="0 0 20 8" aria-hidden className="shrink-0">
      <line
        x1="1"
        y1="4"
        x2="19"
        y2="4"
        stroke={color}
        strokeWidth="2"
        strokeDasharray={dash}
        strokeLinecap="round"
      />
    </svg>
  )
}

function ChartTooltipContent({
  active,
  payload,
  label,
  labelFormatter,
  valueFormatter,
}: ChartTooltipContentProps) {
  // Series en hueco (null) no aparecen; sin ningun valor no hay tooltip.
  const entries = (payload ?? []).filter((e) => typeof e.value === "number")
  if (!active || entries.length === 0 || label === undefined) return null

  return (
    <div className="grid min-w-36 gap-1.5 rounded-md border border-border bg-surface px-2.5 py-2 text-xs text-text">
      <div className="font-semibold">{labelFormatter(label)}</div>
      {entries.map((e) => (
        <div key={String(e.name)} className="flex items-center gap-2">
          <LineSwatch color={e.stroke ?? "currentColor"} dash={e.strokeDasharray} />
          <span className="text-text-muted">{e.name}</span>
          <span className="ml-auto pl-3 font-medium">{valueFormatter(e.value as number)}</span>
        </div>
      ))}
    </div>
  )
}

export { ChartContainer, ChartTooltip, ChartTooltipContent, LineSwatch }
