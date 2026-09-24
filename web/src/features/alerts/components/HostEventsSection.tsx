import { useState } from 'react'
import { IconChevronDown } from '@tabler/icons-react'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { InlineError } from '@/components/ui/inline-error'
import { Skeleton } from '@/components/ui/skeleton'
import { useHostEvents } from '../hooks/useAlerts'
import { HostEventsList } from './HostEventsList'

function EventsContent({ hostId }: { hostId: string }) {
  const { data, error } = useHostEvents(hostId)
  if (error) return <InlineError>{error}</InlineError>
  if (!data) return <Skeleton className="h-16 w-full" />
  return <HostEventsList events={data} />
}

/** Sección plegable del detalle de host; solo consulta la API mientras está abierta. */
export function HostEventsSection({ hostId }: { hostId: string }) {
  const [open, setOpen] = useState(false)
  return (
    <Collapsible open={open} onOpenChange={setOpen} className="flex flex-col gap-3">
      <CollapsibleTrigger className="flex w-fit items-center gap-1.5 rounded-md text-sm font-semibold outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <IconChevronDown className={`size-4 shrink-0 transition-transform ${open ? '' : '-rotate-90'}`} stroke={1.5} aria-hidden />
        Historial de estado
      </CollapsibleTrigger>
      <CollapsibleContent>{open && <EventsContent hostId={hostId} />}</CollapsibleContent>
    </Collapsible>
  )
}
