import { Suspense, useState } from 'react'
import { Navigate, Outlet, useLocation, useNavigate } from 'react-router'
import { BackLink } from '@/components/ui/back-link'
import { InlineError } from '@/components/ui/inline-error'
import { Skeleton } from '@/components/ui/skeleton'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { useBreadcrumbTrail } from '@/features/layout/hooks/breadcrumbContext'
import { useHostDetail } from '../hooks/useHostDetail'
import { useIsCollector } from '../hooks/useIsCollector'
import type { HostOutlet } from '../hooks/useHostOutlet'
import type { HostActionId } from '../utils/hostActions'
import { hostTabPath, legacyTabPath, tabForPath } from '../utils/tabs'
import { HostActionDialogs } from './HostActionDialogs'
import { HostHeader } from './HostHeader'
import { HostNav } from './HostNav'

/**
 * Layout de `/hosts/:id`: cabecera y barra de secciones fijas; cada sección es una sub-ruta que se pinta en el
 * `Outlet`. Un dispositivo sin agente tiene su propia ruta (`/dispositivos/:id`).
 */
export function HostLayout({ id }: { id: string }) {
  const detail = useHostDetail(id)
  const { search } = useLocation()
  const legacy = legacyTabPath(id, new URLSearchParams(search).get('tab'))
  if (legacy) return <Navigate to={legacy} replace />
  if (detail.data?.host.kind === 'device') return <Navigate to={`/dispositivos/${encodeURIComponent(id)}`} replace />
  // Hasta conocer el tipo no se monta la vista de agente (pediria datos que un dispositivo no tiene).
  if (detail.loading) {
    return (
      <div className="flex flex-col gap-3">
        <BackLink to="/hosts">Hosts</BackLink>
        <p className="text-sm text-text-muted">Cargando…</p>
      </div>
    )
  }
  return <AgentHostLayout id={id} detail={detail} />
}

function AgentHostLayout({ id, detail }: { id: string; detail: HostOutlet['detail'] }) {
  const { user } = useAuth()
  const [action, setAction] = useState<HostActionId | null>(null)
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const isCollector = useIsCollector(id)
  const data = detail.data

  // 400 (id invalido) y 404 son definitivos: no hay nada mas que mostrar.
  const notFound = detail.errorStatus === 400 || detail.errorStatus === 404
  const tab = tabForPath(id, pathname)
  useBreadcrumbTrail(
    data
      ? tab.path
        ? [{ label: data.host.hostname, to: hostTabPath(id, '') }, { label: tab.label }]
        : [{ label: data.host.hostname }]
      : null,
  )

  return (
    <div className="flex flex-col gap-3">
      <BackLink to="/hosts">Hosts</BackLink>

      {notFound ? (
        <InlineError>Host no encontrado.</InlineError>
      ) : (
        <>
          {data && (
            <HostHeader
              host={data.host}
              ip={data.info.ips[0] ?? null}
              onAction={user?.role === 'admin' ? setAction : undefined}
            />
          )}
          {detail.error && <InlineError>{detail.error}</InlineError>}

          <HostNav hostId={id} isCollector={isCollector === true} />
          <Suspense fallback={<Skeleton className="h-64 w-full" />}>
            <Outlet context={{ hostId: id, detail, isCollector } satisfies HostOutlet} />
          </Suspense>

          {data && (
            <HostActionDialogs
              host={action ? data.host : null}
              action={action}
              onClose={() => setAction(null)}
              onDone={(done) => {
                setAction(null)
                if (done === 'delete') void navigate('/hosts')
                else void detail.refresh()
              }}
            />
          )}
        </>
      )}
    </div>
  )
}
