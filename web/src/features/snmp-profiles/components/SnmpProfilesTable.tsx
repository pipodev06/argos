import { KeyRoundIcon, LockIcon, LockOpenIcon, PencilIcon, Trash2Icon } from 'lucide-react'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell } from '@/components/data-table/DataTable'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { formatFullDate } from '@/features/hosts/utils/format'
import type { SnmpProfile } from '../types/snmpProfile.types'
import { versionLabel } from '../utils/profileForm'

export type SnmpProfileAction = 'edit' | 'delete'

interface SnmpProfilesTableProps {
  profiles: SnmpProfile[]
  onAction: (action: SnmpProfileAction, profile: SnmpProfile) => void
}

function VersionBadge({ profile }: { profile: SnmpProfile }) {
  return <Badge Icon={profile.version === 3 ? LockIcon : LockOpenIcon}>{versionLabel(profile.version)}</Badge>
}

const protocols = (p: SnmpProfile): string => (p.auth_proto && p.priv_proto ? `${p.auth_proto} / ${p.priv_proto}` : '—')

export function SnmpProfilesTable({ profiles, onAction }: SnmpProfilesTableProps) {
  return (
    <DataTable caption="Perfiles SNMP" className="min-w-[56rem]">
      <thead>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>Nombre</HeadCell>
          <HeadCell>Versión</HeadCell>
          <HeadCell>Usuario</HeadCell>
          <HeadCell>Protocolos</HeadCell>
          <HeadCell>Dispositivos</HeadCell>
          <HeadCell>Secreto</HeadCell>
          <HeadCell>Creado</HeadCell>
          <HeadCell>Acciones</HeadCell>
        </HeadRow>
      </thead>
      <tbody>
        {profiles.map((p, i) => (
          <Row key={p.id}>
            <RowNumberCell n={i + 1} />
            <Cell className="max-w-56 truncate font-semibold">{p.name}</Cell>
            <Cell>
              <VersionBadge profile={p} />
            </Cell>
            <Cell className="max-w-40 truncate">{p.username ?? '—'}</Cell>
            <Cell>{protocols(p)}</Cell>
            <Cell className="tabular-nums">{p.device_count}</Cell>
            <Cell>
              {p.has_secret || p.has_community ? (
                <span className="inline-flex items-center gap-1 text-xs">
                  <KeyRoundIcon className="size-4 shrink-0 text-text-muted" strokeWidth={1.5} aria-hidden />
                  Sí
                </span>
              ) : (
                <span className="text-text-muted">No</span>
              )}
            </Cell>
            <Cell className="text-sm text-text-muted tabular-nums">{formatFullDate(p.created_at)}</Cell>
            <Cell>
              <Button variant="ghost" size="icon-sm" aria-label={`Editar perfil ${p.name}`} title="Editar" onClick={() => onAction('edit', p)}>
                <PencilIcon strokeWidth={1.5} aria-hidden />
              </Button>
              <Button variant="ghost" size="icon-sm" aria-label={`Eliminar perfil ${p.name}`} title="Eliminar" onClick={() => onAction('delete', p)}>
                <Trash2Icon strokeWidth={1.5} aria-hidden />
              </Button>
            </Cell>
          </Row>
        ))}
      </tbody>
    </DataTable>
  )
}
