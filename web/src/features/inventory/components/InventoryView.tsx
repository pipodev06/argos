import { useCallback, useState } from 'react'
import { InlineError } from '@/components/ui/inline-error'
import { BackLink } from '@/components/ui/back-link'
import { useHostDetail } from '@/features/hosts/hooks/useHostDetail'
import { InventoryContent } from './InventoryContent'

/** Página de inventario del host: encabezado con el nombre, enlace de vuelta y el contenido por sub-sección. */
export function InventoryView({ id }: { id: string }) {
  const detail = useHostDetail(id)
  const [denied, setDenied] = useState(false)
  const onDenied = useCallback(() => setDenied(true), [])
  const notFound = detail.errorStatus === 400 || detail.errorStatus === 404

  return (
    <div className="flex flex-col gap-6">
      <BackLink to={`/hosts/${encodeURIComponent(id)}`}>Volver al host</BackLink>
      {notFound ? (
        <InlineError>Host no encontrado.</InlineError>
      ) : (
        <>
          <h1 className="min-w-0 truncate text-lg font-semibold">
            Inventario{detail.data ? ` · ${detail.data.host.hostname}` : ''}
          </h1>
          {denied ? <InlineError>Inventario no disponible.</InlineError> : <InventoryContent hostId={id} onDenied={onDenied} />}
        </>
      )}
    </div>
  )
}
