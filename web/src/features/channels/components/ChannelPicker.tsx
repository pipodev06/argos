import { Link } from 'react-router'
import { Checkbox } from '@/components/ui/checkbox'
import { InlineError } from '@/components/ui/inline-error'
import { useChannels } from '../hooks/useChannels'

interface ChannelPickerProps {
  selected: string[]
  onChange: (ids: string[]) => void
  /** Título del grupo (por defecto, canales normales). */
  legend?: string
  /** Prefijo de los ids de los checkboxes; distinto si hay dos pickers en el mismo formulario. */
  idPrefix?: string
}

/** Multiselección de canales existentes (checkboxes) para una regla. */
export function ChannelPicker({ selected, onChange, legend = 'Canales de notificación', idPrefix = 'rule-channel' }: ChannelPickerProps) {
  const { data: channels, error, loading } = useChannels()

  function toggle(id: string, on: boolean) {
    onChange(on ? [...selected, id] : selected.filter((s) => s !== id))
  }

  return (
    <fieldset className="grid gap-2">
      <legend className="text-sm font-medium text-text">{legend}</legend>
      {error && <InlineError>{error}</InlineError>}
      {loading && !channels && <p className="text-xs text-text-muted">Cargando canales...</p>}
      {channels && channels.length === 0 && (
        <p className="text-sm text-text-muted">
          Aún no hay canales.{' '}
          <Link to="/alertas/canales" className="text-text underline">
            Crear un canal
          </Link>
        </p>
      )}
      {channels?.map((c) => (
        <label key={c.id} htmlFor={`${idPrefix}-${c.id}`} className="flex items-center gap-2 text-sm">
          <Checkbox id={`${idPrefix}-${c.id}`} checked={selected.includes(c.id)} onCheckedChange={(v) => toggle(c.id, v === true)} />
          <span>
            {c.name}
            {!c.enabled && <span className="text-xs text-text-muted"> (desactivado)</span>}
          </span>
        </label>
      ))}
    </fieldset>
  )
}
