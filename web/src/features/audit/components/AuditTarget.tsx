import { CheckIcon, CopyIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useCopyToClipboard } from '@/hooks/useCopyToClipboard'
import { shortId } from '../utils/labels'

interface AuditTargetProps {
  type: string | null
  id: string | null
}

/** Tipo + id abreviado del objetivo, con botón para copiar el id completo. */
export function AuditTarget({ type, id }: AuditTargetProps) {
  const { copied, copy } = useCopyToClipboard()
  if (!type && !id) return <span aria-label="Sin objetivo">—</span>
  return (
    <span className="inline-flex items-center gap-1">
      {type && <span>{type}</span>}
      {id && (
        <>
          <span title={id}>{shortId(id)}</span>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`Copiar id ${id}`}
            title="Copiar id"
            onClick={() => void copy(id)}
          >
            {copied ? <CheckIcon strokeWidth={1.5} aria-hidden /> : <CopyIcon strokeWidth={1.5} aria-hidden />}
          </Button>
          <span role="status" className="sr-only">
            {copied ? 'Copiado' : ''}
          </span>
        </>
      )}
    </span>
  )
}
