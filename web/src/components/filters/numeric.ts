import type { NumericField } from './types'

/** "1 día", "2 horas", "90 minutos": la unidad más grande que divide exacto; si ninguna, el valor base. */
export function formatWithUnits(value: number, units: NonNullable<NumericField['units']>, baseUnit = ''): string {
  for (const u of [...units].sort((a, b) => b.factor - a.factor)) {
    if (value >= u.factor && value % u.factor === 0) {
      const n = value / u.factor
      return `${n} ${n === 1 ? u.one : u.many}`
    }
  }
  return `${value} ${baseUnit}`.trimEnd()
}

/** Texto de la pastilla activa: "CPU > 80 %", "Visto hace > 2 horas". */
export function numericChipText(f: NumericField, value: number): string {
  const shown = f.units ? formatWithUnits(value, f.units, f.baseUnit) : f.suffix ? `${value} ${f.suffix}` : String(value)
  return `${f.label} ${f.op} ${shown}`
}
