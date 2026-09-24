import { IconTag } from '@tabler/icons-react'

/** Etiquetas de solo lectura (chips con borde, texto `text`). */
export function TagChips({ tags }: { tags: readonly string[] }) {
  if (tags.length === 0) return null
  return (
    <ul aria-label="Etiquetas" className="flex flex-wrap gap-1.5">
      {tags.map((t) => (
        <li key={t} className="inline-flex items-center gap-1 rounded-md border border-border px-2 py-0.5 text-xs text-text">
          <IconTag className="size-3.5 shrink-0 text-text-muted" stroke={1.5} aria-hidden />
          {t}
        </li>
      ))}
    </ul>
  )
}
