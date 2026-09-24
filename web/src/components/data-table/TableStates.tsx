import type { Icon } from '@tabler/icons-react'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { InlineError } from '@/components/ui/inline-error'

/** Sin resultados con los filtros actuales: mensaje + "Limpiar". */
export function NoResultsState({ Icon, title, onClear }: { Icon: Icon; title: string; onClear: () => void }) {
  return (
    <EmptyState
      Icon={Icon}
      title={title}
      action={
        <Button variant="outline" size="sm" onClick={onClear}>
          Limpiar
        </Button>
      }
    />
  )
}

/** Error de carga con "Reintentar". */
export function TableError({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="flex flex-wrap items-center gap-3">
      <InlineError>{message}</InlineError>
      <Button variant="outline" size="sm" onClick={onRetry}>
        Reintentar
      </Button>
    </div>
  )
}
