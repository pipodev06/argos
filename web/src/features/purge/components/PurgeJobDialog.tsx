import type { ReactNode } from 'react'
import { CircleCheckIcon, CircleXIcon, LoaderCircleIcon } from 'lucide-react'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { InlineError } from '@/components/ui/inline-error'
import { ProgressBar } from '@/components/ui/progress-bar'
import { isPurgeJobFinal } from '../services/purgeJobPoller'
import type { PurgeJob } from '../types/purge.types'
import { purgeProgressLabel, purgeProgressPct } from '../utils/purge'

interface PurgeJobDialogProps {
  job: PurgeJob
  /** Error definitivo al consultar el trabajo (no se pudo seguir su avance). */
  pollError: string | null
  /** Error al repetir la baja. */
  retryError: string | null
  retrying: boolean
  /** Titulo mientras corre ("Eliminando \"x\""). */
  runningTitle: string
  doneTitle: string
  /** Contenido al terminar bien (conteos). */
  doneContent: ReactNode
  /** Acciones extra al terminar bien (p. ej. constancia). */
  doneActions?: ReactNode
  onRetry: () => void
  /** Cierra el dialogo; si el trabajo sigue en curso, sigue en el servidor. */
  onClose: () => void
}

/** Fase "Eliminando…" de una baja en segundo plano: progreso, resultado o error con "Reintentar". Compartido por host, grupo y dispositivo. */
export function PurgeJobDialog({
  job,
  pollError,
  retryError,
  retrying,
  runningTitle,
  doneTitle,
  doneContent,
  doneActions,
  onRetry,
  onClose,
}: PurgeJobDialogProps) {
  const done = job.state === 'done'
  const failed = job.state === 'failed'
  const active = !isPurgeJobFinal(job)
  const label = purgeProgressLabel(job)
  const StatusIcon = done ? CircleCheckIcon : failed ? CircleXIcon : LoaderCircleIcon

  return (
    <AlertDialog open onOpenChange={(next) => !next && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            <StatusIcon
              className={`size-5 shrink-0 ${done ? 'text-ok' : failed ? 'text-danger' : 'animate-spin motion-reduce:animate-none'}`}
              strokeWidth={1.5}
              aria-hidden
            />
            {done ? doneTitle : failed ? 'La baja falló' : runningTitle}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {done
              ? 'La baja terminó.'
              : failed
                ? (job.error ?? 'La baja falló; reintenta.')
                : 'Puedes cerrar esta ventana: la baja continúa en el servidor.'}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {active && (
          <div className="grid gap-2" role="status">
            <p className="text-sm tabular-nums">{label}</p>
            {job.kind === 'group' && <ProgressBar value={purgeProgressPct(job)} label="Progreso de la baja" valueText={label} />}
          </div>
        )}
        {done && doneContent}
        {pollError && <InlineError>{pollError}</InlineError>}
        {retryError && <InlineError>{retryError}</InlineError>}
        <AlertDialogFooter>
          {failed && (
            <Button type="button" variant="outline" disabled={retrying} onClick={onRetry}>
              {retrying ? 'Reintentando…' : 'Reintentar'}
            </Button>
          )}
          {done && doneActions}
          <AlertDialogAction onClick={onClose}>Cerrar</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
