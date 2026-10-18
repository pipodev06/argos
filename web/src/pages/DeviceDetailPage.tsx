import { useParams } from 'react-router'
import { BackLink } from '@/components/ui/back-link'
import { InlineError } from '@/components/ui/inline-error'
import { DeviceDetailView } from '@/features/devices/components/DeviceDetailView'
import { isHostId } from '@/features/hosts/utils/id'

export function DeviceDetailPage() {
  const { id } = useParams()
  if (!id || !isHostId(id)) {
    return (
      <div className="flex flex-col gap-3">
        <BackLink to="/dispositivos">Dispositivos</BackLink>
        <InlineError>Id de dispositivo inválido.</InlineError>
      </div>
    )
  }
  return <DeviceDetailView key={id} id={id} />
}
