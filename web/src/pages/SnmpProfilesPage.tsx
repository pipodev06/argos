import { LockIcon } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import { PageHeader } from '@/components/ui/page-header'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { SnmpProfilesSection } from '@/features/snmp-profiles/components/SnmpProfilesSection'

/** Solo administradores: un viewer ve el aviso y no se monta nada que llame a la API. */
export function SnmpProfilesPage() {
  const { user } = useAuth()
  if (user?.role !== 'admin') {
    return (
      <>
        <PageHeader title="Perfiles SNMP" />
        <EmptyState Icon={LockIcon} title="Sin permiso" />
      </>
    )
  }
  return <SnmpProfilesSection />
}
