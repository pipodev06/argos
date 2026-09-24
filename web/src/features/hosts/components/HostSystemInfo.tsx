import { useState, type ReactNode } from 'react'
import { IconChevronDown, IconCopy, IconCheck } from '@tabler/icons-react'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { useCopyToClipboard } from '@/hooks/useCopyToClipboard'
import type { HostDetail, HostInfo } from '../types/host.types'
import { formatCapacity, formatFullDate } from '../utils/format'
import { formatOs } from '../utils/os'

function Item({ term, children }: { term: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-xs text-text-muted">{term}</dt>
      <dd className="text-sm break-words">{children ?? '—'}</dd>
    </div>
  )
}

export function HostSystemInfo({ host, info }: { host: HostDetail; info: HostInfo }) {
  const [open, setOpen] = useState(false)
  const { copied, copy } = useCopyToClipboard()
  const os = formatOs(host.os, info.os_name, info.os_version)
  const cores =
    info.cpu_cores != null || info.cpu_threads != null
      ? `${info.cpu_cores ?? '—'} físicos / ${info.cpu_threads ?? '—'} lógicos`
      : null

  return (
    <Collapsible open={open} onOpenChange={setOpen} className="flex flex-col gap-2">
      <CollapsibleTrigger className="flex w-fit items-center gap-1 rounded-md text-sm font-semibold outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <IconChevronDown
          className={`size-4 transition-transform motion-reduce:transition-none ${open ? '' : '-rotate-90'}`}
          stroke={1.5}
          aria-hidden
        />
        Detalles del sistema
      </CollapsibleTrigger>
      <CollapsibleContent>
        <dl className="grid gap-x-6 gap-y-3 rounded-md border border-border bg-surface p-4 sm:grid-cols-2">
          <Item term="Sistema operativo">{os}</Item>
          <Item term="Kernel">{info.kernel_version}</Item>
          <Item term="CPU">{info.cpu_model}</Item>
          <Item term="Núcleos">{cores}</Item>
          <Item term="RAM">{info.mem_total_mb != null ? formatCapacity(info.mem_total_mb) : null}</Item>
          <Item term="Arquitectura">{host.arch || null}</Item>
          <Item term="Direcciones IP">
            {info.ips.length > 0 ? <span className="text-sm">{info.ips.join(', ')}</span> : null}
          </Item>
          <Item term="Versión del agente">{info.agent_version}</Item>
          <Item term="Enrolado">{formatFullDate(host.created_at)}</Item>
          <Item term="ID">
            <span className="inline-flex flex-wrap items-center gap-2">
              <span className="text-sm">{host.id}</span>
              <Button
                type="button"
                variant="outline"
                size="sm"
                aria-label="Copiar ID del host"
                onClick={() => void copy(host.id)}
              >
                {copied ? <IconCheck stroke={1.5} aria-hidden /> : <IconCopy stroke={1.5} aria-hidden />}
                {copied ? 'Copiado' : 'Copiar'}
              </Button>
            </span>
          </Item>
        </dl>
      </CollapsibleContent>
    </Collapsible>
  )
}
