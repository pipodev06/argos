import { PageHeader } from '@/components/ui/page-header'
import { BackToHostsLink } from '@/features/hosts/components/BackToHostsLink'

export function NotFoundPage() {
  return (
    <div className="flex flex-col">
      <PageHeader title="Página no encontrada" />
      <p className="mb-4 text-sm text-text-muted">La dirección no existe.</p>
      <BackToHostsLink>Volver a hosts</BackToHostsLink>
    </div>
  )
}
