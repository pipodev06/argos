import { useEffect, useRef, useState } from 'react'
import { IconSearch } from '@tabler/icons-react'
import { Input } from '@/components/ui/input'
import type { SearchField } from './types'

const SEARCH_DEBOUNCE_MS = 300

interface SearchBoxProps {
  field: SearchField
  value: string
  onCommit: (q: string) => void
}

/** Búsqueda ancha con debounce. Atajo "/" para enfocarla. Para reiniciarla desde fuera, cambia su `key`. */
export function SearchBox({ field, value, onCommit }: SearchBoxProps) {
  const [draft, setDraft] = useState(value)
  const input = useRef<HTMLInputElement>(null)
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Atajo "/": enfoca la búsqueda salvo que ya se esté escribiendo en otro campo.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key !== '/' || e.ctrlKey || e.metaKey || e.altKey) return
      const el = e.target as HTMLElement | null
      if (el && (el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName))) return
      e.preventDefault()
      input.current?.focus()
    }
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('keydown', onKey)
      if (timer.current) clearTimeout(timer.current)
    }
  }, [])

  function handleChange(next: string) {
    setDraft(next)
    if (timer.current) clearTimeout(timer.current)
    timer.current = setTimeout(() => onCommit(next.trim()), SEARCH_DEBOUNCE_MS)
  }

  return (
    <div className="relative min-w-64 flex-1">
      <IconSearch className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-text-muted" stroke={1.5} aria-hidden />
      <Input
        ref={input}
        type="search"
        aria-label={field.ariaLabel}
        aria-keyshortcuts="/"
        placeholder={field.placeholder}
        maxLength={field.maxLength ?? 128}
        value={draft}
        onChange={(e) => handleChange(e.target.value)}
        className="h-8 border-text-muted pl-8"
      />
    </div>
  )
}
