import { useCallback } from 'react'
import { DownloadIcon, PackageIcon } from 'lucide-react'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell, TableBody, TableHeader } from '@/components/data-table/DataTable'
import { ActionsCell, ActionsHeadCell } from '@/components/data-table/RowActions'
import { TableError } from '@/components/data-table/TableStates'
import { Button } from '@/components/ui/button'
import { CopyBlock } from '@/components/ui/copy-block'
import { EmptyState } from '@/components/ui/empty-state'
import { FormHelp, type HelpItem } from '@/components/ui/form-help'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { archLabel, fileKind, getAgentManifest, OS_LABELS, sortFiles } from '@/features/agent-dist/agentDist'
import { formatBytes } from '@/features/retention/utils/retention'
import { usePolledFetch } from '@/hooks/usePolledFetch'

const ONCE = { intervalMs: 300_000, paused: true } as const

const HELP: readonly HelpItem[] = [
  ['Cuál descargar', 'El de tu sistema y arquitectura. Windows y Linux casi siempre x64; Mac con chip M1 o posterior: Apple Silicon.'],
  ['Instalar como servicio', 'Como administrador (Windows) o root (Linux/macOS): install → enroll → start. El token lo genera Hosts → Agregar host, que también arma el comando completo con las opciones (inventario, sondeo, privilegios).'],
  ['Windows', '.\\argos-agent.exe install; luego \'<token>\' | .\\argos-agent.exe enroll --server <URL> --token-stdin; luego .\\argos-agent.exe start.'],
  ['Linux (.deb)', 'sudo apt install ./argos-agent_*.deb deja el binario instalado; luego sudo argos-agent install, enroll y start como en Windows.'],
  ['Linux y macOS (binario)', 'chmod +x y luego sudo ./argos-agent install, enroll y start.'],
  ['Verificar', 'Compara el SHA-256 del archivo descargado con el de la tabla (Windows: Get-FileHash; Linux/macOS: sha256sum o shasum -a 256).'],
  ['Actualizar', 'Con el agente ya instalado, ejecuta install con la versión nueva: reemplaza el binario y conserva el enrolamiento.'],
]

/** Descarga de los binarios del agente que publica este server (para instalar en los equipos). */
export function AgentDownloadsPage() {
  const fetcher = useCallback((signal: AbortSignal) => getAgentManifest(signal), [])
  const { data, error, loading, refresh } = usePolledFetch(fetcher, ONCE)

  return (
    <>
      <PageHeader title="Descargar agente" help={<FormHelp title="Descargar agente" items={HELP} />} />
      <div className="flex flex-col gap-3" aria-busy={loading}>
        {error && <TableError message={error} onRetry={() => void refresh()} />}
        {!data && !error && <Skeleton className="h-40 w-full" />}
        {data && data.files.length === 0 && <EmptyState Icon={PackageIcon} title="Sin binarios publicados en este servidor" />}
        {data && data.files.length > 0 && (
          <>
            <p className="text-sm text-text-muted">
              Versión <span className="font-semibold text-text tabular-nums">{data.version}</span>
            </p>
            <DataTable caption="Binarios del agente" className="min-w-[48rem]">
              <TableHeader>
                <HeadRow>
                  <RowNumberHeadCell />
                  <HeadCell>Sistema</HeadCell>
                  <HeadCell>Arquitectura</HeadCell>
                  <HeadCell>Tipo</HeadCell>
                  <HeadCell>Tamaño</HeadCell>
                  <HeadCell>SHA-256</HeadCell>
                  <ActionsHeadCell />
                </HeadRow>
              </TableHeader>
              <TableBody>
                {sortFiles(data.files).map((f, i) => (
                  <Row key={f.name}>
                    <RowNumberCell n={i + 1} />
                    <Cell className="font-semibold">{OS_LABELS[f.os] ?? f.os}</Cell>
                    <Cell>{archLabel(f.os, f.arch)}</Cell>
                    <Cell>{fileKind(f.name)}</Cell>
                    <Cell className="tabular-nums">{formatBytes(f.size)}</Cell>
                    <Cell className="max-w-72">
                      <CopyBlock value={f.sha256} label={`SHA-256 de ${f.name}`} />
                    </Cell>
                    <ActionsCell>
                      <Button asChild variant="ghost" size="icon-sm">
                        <a href={`/dl/${encodeURIComponent(data.version)}/${encodeURIComponent(f.name)}`} download aria-label={`Descargar ${f.name}`} title="Descargar">
                          <DownloadIcon strokeWidth={1.5} aria-hidden />
                        </a>
                      </Button>
                    </ActionsCell>
                  </Row>
                ))}
              </TableBody>
            </DataTable>
          </>
        )}
      </div>
    </>
  )
}
