import { useState } from 'react'
import { IconListCheck } from '@tabler/icons-react'
import { TableError } from '@/components/data-table/TableStates'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/ui/confirm-dialog'
import { EmptyState } from '@/components/ui/empty-state'
import { InlineError } from '@/components/ui/inline-error'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { RuleFormDialog } from '@/features/alerts/components/RuleFormDialog'
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
  const [form, setForm] = useState<{ rule: AlertRule | null; clone?: boolean } | null>(null)
  const [deleting, setDeleting] = useState<AlertRule | null>(null)
  const del = useUserMutation()
  const toggle = useUserMutation()
  const mute = useUserMutation()

  const disabledCount = rules?.filter((r) => !r.enabled).length ?? 0

  function handleAction(action: RuleAction, rule: AlertRule) {
    if (action === 'edit') setForm({ rule })
    else if (action === 'clone') setForm({ rule, clone: true })
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
        actions={isAdmin && <Button size="sm" onClick={() => setForm({ rule: null })}>Nueva regla</Button>}
      />
      <div className="flex flex-col gap-4" aria-busy={loading}>
        {error && <TableError message={error} onRetry={() => void refresh()} />}
        {toggle.error && <InlineError>{toggle.error}</InlineError>}
        {mute.error && <InlineError>{mute.error}</InlineError>}
        {disabledCount > 0 && (
          <p className="text-sm text-text-muted">
            {disabledCount === 1 ? '1 regla está desactivada' : `${disabledCount} reglas están desactivadas`}: no generan
            alertas hasta que{isAdmin ? ' las actives' : ' un administrador las active'}.
          </p>
        )}
        {loading && !rules && <Skeleton className="h-40 w-full" />}
        {rules && rules.length === 0 && (
          <EmptyState Icon={IconListCheck} title="Sin reglas" />
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
          <RuleFormDialog
            rule={form?.rule ?? null}
            clone={form?.clone}
            open={form !== null}
            onClose={() => setForm(null)}
            onSaved={() => {
              setForm(null)
              void refresh()
            }}
          />
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
