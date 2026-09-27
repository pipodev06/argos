import { useState } from 'react'
import { IconAlertTriangle } from '@tabler/icons-react'
import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { CopyBlock } from '@/components/ui/copy-block'
import { DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useNow } from '@/hooks/useNow'
import { AgentOptionsForm } from './AgentOptionsForm'
import type { EnrollToken, HostnameConflict } from '../types/host.types'
import { applyAgentOptions, DEFAULT_AGENT_OPTIONS, enrollCommands, isHttpOrigin, minutesLeft, parseNets, type AgentOptions } from '../utils/enroll'
import { formatExpiry } from '../utils/format'
import { HOST_STATUS_LABELS } from '../utils/status'

interface EnrollTokenStepProps {
  token: EnrollToken
  title: string
  onClose: () => void
  onAnother: () => void
}

/** Paso "aquí tienes el comando y el token" compartido por alta normal y re-enrolar/reactivar. */
export function EnrollTokenStep({ token, title, onClose, onAnother }: EnrollTokenStepProps) {
  // Cuenta regresiva cada 30 s, sin aria-live (no interrumpe a lectores de pantalla).
  const now = useNow(30_000)
  const left = minutesLeft(token.expires_at, now)
  const origin = window.location.origin
  const showInsecure = isHttpOrigin(origin)
  const commands = enrollCommands(token, origin)
  const [options, setOptions] = useState<AgentOptions>(DEFAULT_AGENT_OPTIONS)
  const effective = { ...options, insecure: options.insecure && showInsecure }
  const netsResult = options.probe ? parseNets(options.probeNets) : null
  // Con la casilla recién marcada y el campo vacío no se grita: el error aparece al escribir; la copia sigue bloqueada.
  const netsError = netsResult && !netsResult.ok && options.probeNets.trim() !== '' ? netsResult.error : null
  const windows = applyAgentOptions(commands.windows, 'windows', effective)
  const linux = applyAgentOptions(commands.linux, 'linux', effective)

  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-4">
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>
          {left > 0 ? (
            <>
              Se usa una sola vez y expira a las{' '}
              <span className="tabular-nums">{formatExpiry(token.expires_at)}</span> (en {left} min).
            </>
          ) : (
            'El token expiró.'
          )}
        </DialogDescription>
      </DialogHeader>

      {token.hostname_conflict && <HostnameConflictNotice conflict={token.hostname_conflict} />}

      {left > 0 ? (
        <>
          <AgentOptionsForm value={options} onChange={setOptions} netsError={netsError} showInsecure={showInsecure} />
          <Tabs defaultValue="windows">
            <TabsList aria-label="Sistema operativo">
              <TabsTrigger value="windows">Windows (PowerShell)</TabsTrigger>
              <TabsTrigger value="linux">Linux / macOS (bash)</TabsTrigger>
            </TabsList>
            <TabsContent value="windows">
              {windows.ok ? <CopyBlock value={windows.command} label="Comando para Windows" /> : <BlockedCommand />}
            </TabsContent>
            <TabsContent value="linux">
              {linux.ok ? <CopyBlock value={linux.command} label="Comando para Linux o macOS" /> : <BlockedCommand />}
            </TabsContent>
          </Tabs>
          <div className="grid gap-1">
            <p className="text-sm font-medium">Token</p>
            <CopyBlock value={token.enroll_token} label="Token de enrolamiento" />
          </div>
          <p className="flex items-start gap-1.5 text-sm">
            <IconAlertTriangle className="mt-0.5 size-4 shrink-0 text-warn" stroke={1.5} aria-hidden />
            Trátalo como una contraseña: no lo pegues en chats, tickets ni correos. Quien lo tenga puede registrar un
            equipo falso.
          </p>
          <p className="text-xs text-text-muted">Cierra este diálogo y no podrás volver a verlo.</p>
        </>
      ) : (
        <div className="flex flex-col items-start gap-2">
          <p className="text-sm text-text-muted">Genera otro token para continuar.</p>
          <Button type="button" variant="outline" size="sm" onClick={onAnother}>
            Generar otro
          </Button>
        </div>
      )}

      <DialogFooter>
        <Button type="button" onClick={onClose}>
          Cerrar
        </Button>
      </DialogFooter>
    </div>
  )
}

function HostnameConflictNotice({ conflict }: { conflict: HostnameConflict }) {
  return (
    <p className="flex items-start gap-1.5 text-sm">
      <IconAlertTriangle className="mt-0.5 size-4 shrink-0 text-warn" stroke={1.5} aria-hidden />
      <span>
        Ya existe un host con este nombre (estado: {HOST_STATUS_LABELS[conflict.status]}). Si es el mismo equipo,
        considera{' '}
        <Link
          to={`/hosts/${encodeURIComponent(conflict.host_id)}`}
          className="rounded-md text-brand underline-offset-4 outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring"
        >
          reactivarlo o re-enrolarlo
        </Link>{' '}
        en vez de crear uno nuevo.
      </span>
    </p>
  )
}

function BlockedCommand() {
  return (
    <p role="status" className="text-sm text-text-muted">
      Corrige las redes permitidas para ver y copiar el comando.
    </p>
  )
}
