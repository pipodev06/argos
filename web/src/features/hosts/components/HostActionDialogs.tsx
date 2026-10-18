import { PurgeHostDialog } from '@/features/purge/components/PurgeHostDialog'
import type { HostDetail } from '../types/host.types'
import type { HostActionId } from '../utils/hostActions'
import { EditHostDialog } from './EditHostDialog'
import { ReEnrollHostDialog } from './ReEnrollHostDialog'
import { RevokeHostDialog } from './RevokeHostDialog'

/** Host sobre el que se actua: lo que tienen en comun la fila de la tabla y el detalle. */
type ActionHost = Pick<HostDetail, 'id' | 'hostname' | 'status'>

interface HostActionDialogsProps {
  host: ActionHost | null
  action: HostActionId | null
  onClose: () => void
  /** Termino la accion (guardado, deshabilitado, re-enrolado o eliminado). */
  onDone: (action: HostActionId) => void
}

/** Dialogos de las acciones de host: los mismos para la tabla de Hosts y el detalle. */
export function HostActionDialogs({ host, action, onClose, onDone }: HostActionDialogsProps) {
  const target = (id: HostActionId) => (action === id ? host : null)
  return (
    <>
      <EditHostDialog host={target('edit')} onClose={onClose} onSaved={() => onDone('edit')} />
      <RevokeHostDialog host={target('disable')} onClose={onClose} onDone={() => onDone('disable')} />
      <ReEnrollHostDialog host={target('reenroll')} onClose={() => onDone('reenroll')} />
      <PurgeHostDialog host={target('delete')} onClose={onClose} onDone={() => onDone('delete')} />
    </>
  )
}
