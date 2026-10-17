import { useState } from 'react'
import { FoldersIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { InlineError } from '@/components/ui/inline-error'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { DeleteGroupDialog } from '@/features/groups/components/DeleteGroupDialog'
import { GroupFormDialog } from '@/features/groups/components/GroupFormDialog'
import { GroupsTable } from '@/features/groups/components/GroupsTable'
import { useGroups } from '@/features/groups/hooks/useGroups'
import { PurgeGroupDialog } from '@/features/purge/components/PurgeGroupDialog'
import type { Group } from '@/features/groups/types/group.types'

export function GroupsPage() {
  const { user } = useAuth()
  const { groups, error, loading, refresh } = useGroups()
  const canEdit = user?.role === 'admin'
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<Group | null>(null)
  const [deleting, setDeleting] = useState<Group | null>(null)
  const [purging, setPurging] = useState<Group | null>(null)

  function openForm(group: Group | null) {
    setEditing(group)
    setFormOpen(true)
  }

  return (
    <>
      <PageHeader
        title="Grupos"
        actions={canEdit && <Button size="sm" onClick={() => openForm(null)}>Nuevo grupo</Button>}
      />
      <div className="flex flex-col gap-4" aria-busy={loading}>
        {error && (
          <div className="flex flex-wrap items-center gap-3">
            <InlineError>{error}</InlineError>
            <Button variant="outline" size="sm" onClick={() => void refresh()}>Reintentar</Button>
          </div>
        )}
        {loading && <Skeleton className="h-40 w-full" />}
        {groups && groups.items.length === 0 && (
          <EmptyState
            Icon={FoldersIcon}
            title="Sin grupos"
          />
        )}
        {groups && (groups.items.length > 0 || groups.ungrouped.host_count > 0) && (
          <GroupsTable
            groups={groups.items}
            ungroupedCount={groups.ungrouped.host_count}
            defaultDownAfter={groups.defaults.down_after_seconds}
            canEdit={canEdit}
            onEdit={openForm}
            onDelete={(g) => (g.host_count > 0 ? setPurging(g) : setDeleting(g))}
          />
        )}
      </div>
      {canEdit && (
        <>
          <GroupFormDialog
            group={editing}
            open={formOpen}
            defaultDownAfter={groups?.defaults.down_after_seconds ?? 300}
            onClose={() => setFormOpen(false)}
            onSaved={() => {
              setFormOpen(false)
              void refresh()
            }}
          />
          <DeleteGroupDialog
            group={deleting}
            onClose={() => setDeleting(null)}
            onDeleted={() => {
              setDeleting(null)
              void refresh()
            }}
          />
          <PurgeGroupDialog
            group={purging}
            onClose={() => setPurging(null)}
            onDone={() => {
              setPurging(null)
              void refresh()
            }}
          />
        </>
      )}
    </>
  )
}
