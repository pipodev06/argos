import { useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { ListChecksIcon } from 'lucide-react'
import { TableError } from '@/components/data-table/TableStates'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { InlineError } from '@/components/ui/inline-error'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { RulesTable, type RuleAction } from '@/features/alerts/components/RulesTable'
import { useAlertRules } from '@/features/alerts/hooks/useAlerts'
import { deleteRule, muteRule, unmuteRule, updateRule } from '@/features/alerts/services/alerts.service'
import type { AlertRule } from '@/features/alerts/types/alert.types'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { useUserMutation } from '@/features/users/hooks/useUserMutation'

export function RulesPage() {
  const { user } = useAuth()
  const isAdmin = user?.role === 'admin'
  const { data: rules, error, loading, refresh } = useAlertRules()
  const navigate = useNavigate()
  const [deleting, setDeleting] = useState<AlertRule | null>(null)
  const del = useUserMutation()
  const toggle = useUserMutation()
  const mute = useUserMutation()

  function handleAction(action: RuleAction, rule: AlertRule) {
    if (action === 'edit') navigate(`/alertas/reglas/${rule.id}`)
    else if (action === 'clone') navigate(`/alertas/reglas/nueva?clonar=${rule.id}`)
    else if (action === 'delete') setDeleting(rule)
    else void toggle.run(() => updateRule(rule.id, { enabled: !rule.enabled })).then((ok) => ok && void refresh())
  }

  function handleMute(rule: AlertRule, minutes: number) {
    void mute.run(() => muteRule(rule.id, minutes)).then((ok) => ok && void refresh())
  }

  function handleUnmute(rule: AlertRule) {
    void mute.run(() => unmuteRule(rule.id)).then((ok) => ok && void refresh())
  }

  async function confirmDelete() {
    if (!deleting) return
    if (await del.run(() => deleteRule(deleting.id))) {
      setDeleting(null)
      void refresh()
    }
  }

  return (
    <>
      <PageHeader
        title="Reglas de alerta"
        actions={
          isAdmin && (
            <Button asChild size="sm">
              <Link to="/alertas/reglas/nueva">Nueva regla</Link>
            </Button>
          )
        }
      />
      <div className="flex flex-col gap-4" aria-busy={loading}>
        {error && <TableError message={error} onRetry={() => void refresh()} />}
        {toggle.error && <InlineError>{toggle.error}</InlineError>}
        {mute.error && <InlineError>{mute.error}</InlineError>}
        {loading && !rules && <Skeleton className="h-40 w-full" />}
        {rules && rules.length === 0 && (
          <EmptyState Icon={ListChecksIcon} title="Sin reglas" />
        )}
        {rules && rules.length > 0 && (
          <RulesTable
            rules={rules}
            canAct={isAdmin}
            busy={toggle.busy || mute.busy}
            onAction={handleAction}
            onMute={handleMute}
            onUnmute={handleUnmute}
          />
        )}
      </div>
      {isAdmin && (
        <>
          <ConfirmDialog
            open={deleting !== null}
            title={`Eliminar "${deleting?.name ?? ''}"`}
            description="La regla se elimina de forma definitiva y sus alertas abiertas se resuelven."
            confirmLabel="Eliminar"
            destructive
            busy={del.busy}
            error={del.error}
            onConfirm={() => void confirmDelete()}
            onClose={() => {
              del.clearError()
              setDeleting(null)
            }}
          />
        </>
      )}
    </>
  )
}
