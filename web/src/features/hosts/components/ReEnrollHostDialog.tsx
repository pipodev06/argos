import { useEffect } from 'react'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { InlineError } from '@/components/ui/inline-error'
import { EnrollTokenStep } from './EnrollTokenStep'
import { useReEnrollHost } from '../hooks/useReEnrollHost'
import type { HostDetail } from '../types/host.types'

interface ReEnrollHostDialogProps {
  /** El host a re-enrolar/reactivar; null = cerrado. */
  host: Pick<HostDetail, 'id' | 'hostname' | 'status'> | null
  onClose: () => void
}

/** Re-enrola (host.status !== 'disabled') o reactiva (host.status === 'disabled') un host: mismo flujo de token que el alta. */
export function ReEnrollHostDialog({ host, onClose }: ReEnrollHostDialogProps) {
  return (
    <Dialog open={host !== null} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-2xl">{host && <ReEnrollBody host={host} onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}

function ReEnrollBody({ host, onClose }: { host: Pick<HostDetail, 'id' | 'hostname' | 'status'>; onClose: () => void }) {
  const { token, error, submitting, submit, reset } = useReEnrollHost(host.id)
  const isDisabled = host.status === 'disabled'
  const title = isDisabled ? 'Reactivar host' : 'Re-enrolar host'

  useEffect(() => {
    void submit()
    // Se ejecuta una sola vez al montar (el componente se desmonta al cerrar el dialogo).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (token) {
    return (
      <EnrollTokenStep
        token={token}
        title={isDisabled ? 'Instala el agente (host reactivado)' : 'Instala el agente (re-enrolado)'}
        onClose={onClose}
        onAnother={() => {
          reset()
          void submit()
        }}
      />
    )
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>Genera un token nuevo; el historial de este host se conserva.</DialogDescription>
      </DialogHeader>
      {error ? <InlineError>{error}</InlineError> : <p className="text-sm text-text-muted">{submitting ? 'Generando...' : ''}</p>}
    </>
  )
}
