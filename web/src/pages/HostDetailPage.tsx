import { useParams } from 'react-router'
import { InlineError } from '@/components/ui/inline-error'
import { BackLink } from '@/components/ui/back-link'
import { HostDetailView } from '@/features/hosts/components/HostDetailView'
import { isHostId } from '@/features/hosts/utils/id'

export function HostDetailPage() {
  const { id } = useParams()

  if (!id || !isHostId(id)) {
    return (
      <div className="flex flex-col gap-4">
        <BackLink to="/hosts">Hosts</BackLink>
        <InlineError>Id de host inválido.</InlineError>
      </div>
    )
  }
  // key: al cambiar de host se reinicia el estado (rango) del detalle.
  return <HostDetailView key={id} id={id} />
}
