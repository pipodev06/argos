import type { ComponentProps } from "react"
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
}

/** Tile de contador: icono de estado + etiqueta `xs` muted + cifra `2xl` tabular (sobre `StatCard`). */
function StatusTile({ label, value, Icon, iconClassName, alert }: StatusTileProps) {
  return (
    <StatCard label={label} Icon={Icon} iconClassName={iconClassName} alert={alert}>
      <StatValue>{value.toLocaleString("es-PE")}</StatValue>
    </StatCard>
  )
}

/** Clases del enlace que envuelve una tarjeta (foco visible, mismo radio). */
const TILE_LINK =
  "block rounded-md outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-bg"

export { StatusTile, TILE_LINK }
