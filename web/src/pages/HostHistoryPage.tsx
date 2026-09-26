import { useParams } from 'react-router'
import { InlineError } from '@/components/ui/inline-error'
import { BackToHostsLink } from '@/features/hosts/components/BackToHostsLink'
import { HostHistoryView } from '@/features/hosts/components/HostHistoryView'
import { isHostId } from '@/features/hosts/utils/id'

export function HostHistoryPage() {
  const { id } = useParams()
  if (!id || !isHostId(id)) {
    return (
      <div className="flex flex-col gap-6">
        <BackToHostsLink />
        <InlineError>Id de host inválido.</InlineError>
      </div>
    )
  }
  return <HostHistoryView key={id} id={id} />
}
