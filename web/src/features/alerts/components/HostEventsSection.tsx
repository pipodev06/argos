import { InlineError } from '@/components/ui/inline-error'
import { Skeleton } from '@/components/ui/skeleton'
import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { useHostEvents } from '../hooks/useAlerts'
import { HostEventsList } from './HostEventsList'

// El detalle solo muestra lo ultimo: un equipo que se enciende y apaga muchas veces al dia (laptop) genera muchos
// eventos; el historial completo y paginado vive en /hosts/:id/estados.
const RECENT_EVENTS = 5

function EventsContent({ hostId }: { hostId: string }) {
  const { data, error } = useHostEvents(hostId, RECENT_EVENTS)
  if (error) return <InlineError>{error}</InlineError>
  if (!data) return <Skeleton className="h-16 w-full" />
  return (
    <div className="flex flex-col gap-2">
      <HostEventsList events={data} />
      <Button asChild variant="outline" size="sm" className="w-fit">
        <Link to={`/hosts/${encodeURIComponent(hostId)}/estados`}>Ver historial completo</Link>
      </Button>
    </div>
  )
}

/** Ultimos cambios de estado del host (pestana "Historial de estado"); solo consulta al montarse. */
export function HostEventsSection({ hostId }: { hostId: string }) {
  return <EventsContent hostId={hostId} />
}
