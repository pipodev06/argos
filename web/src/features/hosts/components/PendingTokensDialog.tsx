import { useCallback, useState } from 'react'
import { IconKey, IconTrash } from '@tabler/icons-react'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell } from '@/components/data-table/DataTable'
import { TableError } from '@/components/data-table/TableStates'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { Skeleton } from '@/components/ui/skeleton'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'
import { usePolledFetch } from '@/hooks/usePolledFetch'
import { listEnrollTokens, revokeEnrollToken } from '../services/hosts.service'
import type { PendingEnrollToken } from '../types/host.types'
import { formatFullDate } from '../utils/format'

interface PendingTokensDialogProps {
  open: boolean
  onClose: () => void
}

const OPTIONS = { intervalMs: 30_000, paused: true }

export function PendingTokensDialog({ open, onClose }: PendingTokensDialogProps) {
  // El contenido se monta solo abierto: la consulta corre únicamente con el diálogo visible.
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-3xl">{open && <PendingTokensBody />}</DialogContent>
    </Dialog>
  )
}

function PendingTokensBody() {
  const fetcher = useCallback((signal: AbortSignal) => listEnrollTokens(signal), [])
  const { data, error, loading, refresh } = usePolledFetch(fetcher, OPTIONS)
  const [revoking, setRevoking] = useState<PendingEnrollToken | null>(null)
  const { error: revokeError, busy, run, clearError } = useUserMutation()

  function close() {
    clearError()
    setRevoking(null)
  }

  async function confirm() {
    if (!revoking) return
    const ok = await run(
      () => revokeEnrollToken(revoking.id),
      // 404: ya se usó, expiró o lo revocó otro administrador; la lista se actualiza igual.
      (err) => (err.status === 404 ? 'El token ya no existe (se usó, expiró o lo revocaron). Se actualizó la lista.' : null),
    )
    if (ok) {
      close()
      void refresh()
    }
  }

  return (
    <div className="grid gap-4" aria-busy={loading}>
      <DialogHeader>
        <DialogTitle>Tokens pendientes</DialogTitle>
        <DialogDescription>
          Tokens de enrolamiento generados que aún no se usaron ni expiraron. Revoca los que ya no necesites.
        </DialogDescription>
      </DialogHeader>
      {error && <TableError message={error} onRetry={() => void refresh()} />}
      {loading && !data && <Skeleton className="h-24 w-full" />}
      {data && data.length === 0 && <EmptyState Icon={IconKey} title="Sin tokens pendientes" />}
      {data && data.length > 0 && (
        <DataTable caption="Tokens de enrolamiento pendientes" className="min-w-[40rem]">
          <thead>
            <HeadRow>
              <RowNumberHeadCell />
              <HeadCell>Hostname</HeadCell>
              <HeadCell>Grupo</HeadCell>
              <HeadCell>Creado por</HeadCell>
              <HeadCell>Creado</HeadCell>
              <HeadCell>Expira</HeadCell>
              <HeadCell>Acciones</HeadCell>
            </HeadRow>
          </thead>
          <tbody>
            {data.map((t, i) => (
              <Row key={t.id}>
                <RowNumberCell n={i + 1} />
                <Cell className="max-w-48 truncate font-semibold">{t.hostname}</Cell>
                <Cell className="max-w-40 truncate">{t.group?.name ?? 'Sin grupo'}</Cell>
                <Cell className="max-w-32 truncate">{t.created_by}</Cell>
                <Cell className="tabular-nums">{formatFullDate(t.created_at)}</Cell>
                <Cell className="tabular-nums">{formatFullDate(t.expires_at)}</Cell>
                <Cell>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Revocar token de ${t.hostname}`}
                    title="Revocar"
                    onClick={() => setRevoking(t)}
                  >
                    <IconTrash stroke={1.5} aria-hidden />
                  </Button>
                </Cell>
              </Row>
            ))}
          </tbody>
        </DataTable>
      )}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={() => void refresh()} disabled={loading}>
          Actualizar
        </Button>
      </DialogFooter>
      <ConfirmDialog
        open={revoking !== null}
        title={`Revocar token de "${revoking?.hostname ?? ''}"`}
        description="El token deja de servir para enrolar: si ya se pegó el comando en el host, fallará. Puedes generar otro después."
        confirmLabel="Revocar"
        busy={busy}
        error={revokeError}
        onConfirm={() => void confirm()}
        onClose={close}
      />
    </div>
  )
}
