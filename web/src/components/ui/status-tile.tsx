import type { ComponentProps } from "react"
import { ViewAction } from "@/components/data-table/RowActions"
import { StatCard, StatValue } from "@/components/ui/stat-card"

interface StatusTileProps {
  /** Etiqueta del contador ("Caídos"). */
  label: string
  value: number
  Icon: ComponentProps<typeof StatCard>["Icon"]
  /** Color del icono de estado (`text-ok`, `text-danger`...). */
  iconClassName?: string
  /** Barra de 2px `danger` a la izquierda; se usa cuando el contador cuenta caidos y es > 0. */
  alert?: boolean
  /** Lista filtrada a la que lleva el ojo. */
  to?: string
}

/** Tile de contador: icono de estado + etiqueta `xs` muted + cifra `2xl` tabular (sobre `StatCard`). */
function StatusTile({ label, value, Icon, iconClassName, alert, to }: StatusTileProps) {
  return (
    <StatCard label={label} Icon={Icon} iconClassName={iconClassName} alert={alert} action={to && <ViewAction to={to} label={label.toLowerCase()} />}>
      <StatValue>{value.toLocaleString("es-PE")}</StatValue>
    </StatCard>
  )
}

export { StatusTile }
