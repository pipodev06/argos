interface ProgressBarProps {
  /** Avance 0-100. */
  value: number
  /** Nombre accesible ("Progreso de la baja"). */
  label: string
  /** Texto legible del avance ("Borrando 3 de 10 equipos"); mejora el anuncio de lectores de pantalla. */
  valueText?: string
}

/** Barra de progreso `h-2` (radio 6px, relleno `text`, sin color de estado). La etiqueta la pone quien la usa. */
function ProgressBar({ value, label, valueText }: ProgressBarProps) {
  const pct = Math.min(100, Math.max(0, value))
  return (
    <div
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={pct}
      aria-valuetext={valueText}
      className="h-2 w-full overflow-hidden rounded-md bg-border"
    >
      <div className="h-full bg-text" style={{ width: `${pct}%` }} />
    </div>
  )
}

export { ProgressBar }
