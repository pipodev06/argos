import { useParams } from 'react-router'
import { InlineError } from '@/components/ui/inline-error'
import { HostEventsView } from '@/features/alerts/components/HostEventsView'
import { BackToHostsLink } from '@/features/hosts/components/BackToHostsLink'
import { isHostId } from '@/features/hosts/utils/id'

export function HostEventsPage() {
  const { id } = useParams()
  if (!id || !isHostId(id)) {
    return (
      <div className="flex flex-col gap-6">
        <BackToHostsLink />
        <InlineError>Id de host inválido.</InlineError>
      </div>
    )
  }
  return <HostEventsView key={id} id={id} />
}
