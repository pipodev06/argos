import { useParams } from 'react-router'
import { InlineError } from '@/components/ui/inline-error'
import { BackLink } from '@/components/ui/back-link'
import { HostHistoryView } from '@/features/hosts/components/HostHistoryView'
import { isHostId } from '@/features/hosts/utils/id'

export function HostHistoryPage() {
  const { id } = useParams()
  if (!id || !isHostId(id)) {
    return (
      <div className="flex flex-col gap-4">
        <BackLink to="/hosts">Hosts</BackLink>
        <InlineError>Id de host inválido.</InlineError>
      </div>
    )
  }
  return <HostHistoryView key={id} id={id} />
}
