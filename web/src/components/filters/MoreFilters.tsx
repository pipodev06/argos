import { useEffect, useRef, useState, type FormEvent } from 'react'
import { IconArrowLeft } from '@tabler/icons-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { PILL } from './FilterPill'
import { ITEM } from './styles'
import type { NumericField } from './types'

interface MoreFiltersProps {
  fields: readonly NumericField[]
  /** `value` ya viene en la unidad base de la API. */
  onApply: (key: string, value: number) => void
}

/** Pastilla "Más filtros": pasos Campo -> Valor para los filtros numéricos. Un campo activo se reemplaza. */
export function MoreFilters({ fields, onApply }: MoreFiltersProps) {
  const [open, setOpen] = useState(false)
  const [fieldKey, setFieldKey] = useState<string | null>(null)
  const [num, setNum] = useState('')
  const [unit, setUnit] = useState<string | null>(null)
  const body = useRef<HTMLDivElement>(null)
  const def = fields.find((f) => f.key === fieldKey)

  // Al cambiar de paso el botón pulsado se desmonta: el foco pasa al primer control del nuevo paso.
  useEffect(() => {
    if (open) body.current?.querySelector<HTMLElement>('input, button')?.focus()
  }, [open, fieldKey])

  function handleOpenChange(next: boolean) {
    setOpen(next)
    if (!next) {
      setFieldKey(null)
      setNum('')
      setUnit(null)
    }
  }

  function submit(e: FormEvent) {
    e.preventDefault()
    if (!def) return
    const n = Number(num)
    if (num === '' || !Number.isFinite(n) || n < 0 || (def.max !== undefined && n > def.max)) return
    const factor = def.units?.find((u) => u.id === (unit ?? def.defaultUnit))?.factor
    onApply(def.key, factor ? Math.round(n * factor) : n)
    handleOpenChange(false)
  }

  if (fields.length === 0) return null

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <button type="button" className={`${PILL} justify-between px-3 hover:bg-muted`}>
          <span className="truncate">Más filtros</span>
        </button>
      </PopoverTrigger>
      <PopoverContent aria-label="Más filtros" className={`max-h-80 overflow-y-auto ${def ? 'w-64' : 'w-40'}`}>
        <div ref={body} className="flex flex-col gap-1">
          {!def ? (
            <>
              <p className="flex h-6 items-center px-2 text-xs text-text-muted">Campo</p>
              {fields.map((f) => (
                <button key={f.key} type="button" className={ITEM} onClick={() => setFieldKey(f.key)}>
                  {f.label}
                </button>
              ))}
            </>
          ) : (
            <>
              <button type="button" className={`${ITEM} gap-1.5 text-text-muted`} onClick={() => setFieldKey(null)}>
                <IconArrowLeft className="size-4" stroke={1.5} aria-hidden />
                {def.label} {def.op}
              </button>
              <form onSubmit={submit} className="flex flex-col gap-2 p-2">
                <label className="flex items-center gap-2">
                  <span className="sr-only">{`Valor de ${def.label}`}</span>
                  <Input
                    type="number"
                    inputMode="numeric"
                    min={0}
                    max={def.max}
                    step={1}
                    required
                    value={num}
                    onChange={(e) => setNum(e.target.value)}
                    className="h-8 w-24"
                  />
                  {!def.units && def.suffix && <span className="text-text-muted">{def.suffix}</span>}
                </label>
                {def.units && (
                  <SegmentedControl
                    aria-label="Unidad"
                    options={def.units.map((u) => ({ value: u.id, label: u.label }))}
                    value={unit ?? def.defaultUnit ?? def.units[0].id}
                    onChange={setUnit}
                  />
                )}
                <Button type="submit" size="sm">
                  Aplicar
                </Button>
              </form>
            </>
          )}
        </div>
      </PopoverContent>
    </Popover>
  )
}
