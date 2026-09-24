import { useId, useState, type KeyboardEvent } from 'react'
import { IconTag, IconX } from '@tabler/icons-react'
import { Input } from '@/components/ui/input'
import { useTags } from '../hooks/useTags'
import { MAX_TAG_LENGTH, MAX_TAGS, hasTag, validateTag } from '../utils/tags'

interface TagsInputProps {
  id: string
  label: string
  value: string[]
  onChange: (next: string[]) => void
  /** Tope de etiquetas (por defecto 20). */
  max?: number
  autoFocus?: boolean
}

/** Control de etiquetas: chips quitables + campo con autocompletado (Enter o coma agrega). Compartido por editar host y lote. */
export function TagsInput({ id, label, value, onChange, max = MAX_TAGS, autoFocus }: TagsInputProps) {
  const [text, setText] = useState('')
  const [localError, setLocalError] = useState<string | null>(null)
  const known = useTags()
  const listId = useId()
  const shown = localError

  const fail = setLocalError

  /** Agrega el texto pendiente; devuelve false si es invalido. */
  function commit(raw: string): boolean {
    const t = raw.trim()
    if (t === '') return true
    const problem = validateTag(t)
    if (problem) {
      fail(problem)
      return false
    }
    if (hasTag(value, t)) {
      setText('')
      fail(null)
      return true
    }
    if (value.length >= max) {
      fail(max === 1 ? 'Solo puedes elegir una etiqueta.' : `Máximo ${max} etiquetas.`)
      return false
    }
    // Reutiliza la grafia de una etiqueta existente (sin distinguir mayusculas).
    const canon = known.find((k) => k.name.toLowerCase() === t.toLowerCase())?.name ?? t
    onChange([...value, canon])
    setText('')
    fail(null)
    return true
  }

  function handleKey(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault()
      commit(text)
    } else if (e.key === 'Backspace' && text === '' && value.length > 0) {
      onChange(value.slice(0, -1))
    }
  }

  const suggestions = known.filter((k) => !hasTag(value, k.name))

  return (
    <div className="grid gap-1.5">
      <label htmlFor={id} className="text-sm font-medium text-text">
        {label}
      </label>
      {value.length > 0 && (
        <ul aria-label={`${label} elegidas`} className="flex flex-wrap gap-1.5">
          {value.map((t) => (
            <li key={t} className="inline-flex items-center gap-1 rounded-md border border-border py-0.5 pl-2 text-xs text-text">
              <IconTag className="size-3.5 shrink-0 text-text-muted" stroke={1.5} aria-hidden />
              {t}
              <button
                type="button"
                aria-label={`Quitar etiqueta ${t}`}
                onClick={() => onChange(value.filter((x) => x !== t))}
                className="inline-flex size-5 items-center justify-center rounded-md text-text-muted outline-none hover:text-text focus-visible:ring-2 focus-visible:ring-ring"
              >
                <IconX className="size-3.5" stroke={1.5} aria-hidden />
              </button>
            </li>
          ))}
        </ul>
      )}
      <datalist id={listId}>
        {suggestions.map((k) => (
          <option key={k.name} value={k.name} />
        ))}
      </datalist>
      <Input
        id={id}
        list={listId}
        value={text}
        onChange={(e) => {
          // Una coma (p. ej. al pegar) agrega lo anterior a ella; el resto queda en el campo.
          const v = e.target.value
          const i = v.indexOf(',')
          if (i < 0) setText(v)
          else if (commit(v.slice(0, i))) setText(v.slice(i + 1))
        }}
        onKeyDown={handleKey}
        onBlur={() => {
          if (text.trim() !== '') commit(text)
        }}
        maxLength={MAX_TAG_LENGTH + 8}
        placeholder="Escribe y pulsa Enter"
        autoComplete="off"
        autoFocus={autoFocus}
        aria-invalid={shown ? true : undefined}
        aria-describedby={`${id}-hint`}
      />
      <p id={`${id}-hint`} className={`text-xs ${shown ? 'text-text' : 'text-text-muted'}`} role={shown ? 'alert' : undefined}>
        {shown ?? `Enter o coma agrega. ${value.length}/${max}, hasta ${MAX_TAG_LENGTH} caracteres.`}
      </p>
    </div>
  )
}
