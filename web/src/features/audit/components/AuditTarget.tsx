import { CheckIcon, CopyIcon } from 'lucide-react'
import { Link } from 'react-router'
import { useCopyToClipboard } from '@/hooks/useCopyToClipboard'
import { shortId, targetTypeLabel } from '../utils/labels'

interface AuditTargetProps {
  type: string | null
  id: string | null
  /** Nombre actual del objetivo; null si no tiene nombre o ya no existe. */
  name: string | null
}

/** Tipo + nombre (o id abreviado) del objetivo; hosts y dispositivos enlazan a su detalle. Botón para copiar el id. */
export function AuditTarget({ type, id, name }: AuditTargetProps) {
  const { copied, copy } = useCopyToClipboard()
  if (!type && !id) return <span aria-label="Sin objetivo">—</span>
  const text = name ?? (id ? shortId(id) : null)
  const linkable = name && id && (type === 'host' || type === 'device')
  return (
    <span className="inline-flex items-center gap-1">
      {type && <span className="text-text-muted">{targetTypeLabel(type)}</span>}
      {text &&
        (linkable ? (
          <Link to={`/hosts/${id}`} title={id} className="rounded-md font-semibold underline-offset-2 outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring">
            {text}
          </Link>
        ) : (
          <span title={id ?? undefined}>{text}</span>
        ))}
      {id && (
        <>
          <button
            type="button"
            aria-label={`Copiar id ${id}`}
            title="Copiar id"
            onClick={() => void copy(id)}
            className="inline-flex size-5 items-center justify-center rounded-sm text-text-muted outline-none hover:text-text focus-visible:ring-2 focus-visible:ring-ring"
          >
            {copied ? <CheckIcon className="size-3.5" strokeWidth={1.5} aria-hidden /> : <CopyIcon className="size-3.5" strokeWidth={1.5} aria-hidden />}
          </button>
          <span role="status" className="sr-only">
            {copied ? 'Copiado' : ''}
          </span>
        </>
      )}
    </span>
  )
}
