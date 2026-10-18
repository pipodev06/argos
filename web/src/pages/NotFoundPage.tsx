import { PageHeader } from '@/components/ui/page-header'
import { BackLink } from '@/components/ui/back-link'

export function NotFoundPage() {
  return (
    <div className="flex flex-col">
      <PageHeader title="Página no encontrada" />
      <p className="mb-3 text-sm text-text-muted">La dirección no existe.</p>
      <BackLink to="/hosts">Volver a hosts</BackLink>
    </div>
  )
}
