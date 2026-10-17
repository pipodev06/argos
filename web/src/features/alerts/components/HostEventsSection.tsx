import { useState } from 'react'
import { ChevronDownIcon } from 'lucide-react'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { InlineError } from '@/components/ui/inline-error'
import { Skeleton } from '@/components/ui/skeleton'
import { Link } from 'react-router'
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
      <Link to={`/hosts/${encodeURIComponent(hostId)}/estados`} className="w-fit text-sm text-brand underline-offset-4 hover:underline">
        Ver historial completo
      </Link>
    </div>
  )
}

/** Sección plegable del detalle de host; solo consulta la API mientras está abierta. */
export function HostEventsSection({ hostId }: { hostId: string }) {
  const [open, setOpen] = useState(false)
  return (
    <Collapsible open={open} onOpenChange={setOpen} className="flex flex-col gap-3">
      <CollapsibleTrigger className="flex w-fit items-center gap-1.5 rounded-md text-sm font-semibold outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <ChevronDownIcon className={`size-4 shrink-0 transition-transform ${open ? '' : '-rotate-90'}`} strokeWidth={1.5} aria-hidden />
        Historial de estado
      </CollapsibleTrigger>
      <CollapsibleContent>{open && <EventsContent hostId={hostId} />}</CollapsibleContent>
    </Collapsible>
  )
}
