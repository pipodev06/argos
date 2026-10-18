import { useState } from 'react'
import { TriangleAlertIcon } from 'lucide-react'
import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { CopyBlock } from '@/components/ui/copy-block'
import { DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { FormHelp, type HelpItem } from '@/components/ui/form-help'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useNow } from '@/hooks/useNow'
import { AgentOptionsForm } from './AgentOptionsForm'
import type { EnrollToken, HostnameConflict } from '../types/host.types'
import { buildEnrollCommand, DEFAULT_AGENT_OPTIONS, isHttpOrigin, minutesLeft, parseNets, type AgentOptions } from '../utils/enroll'
import { formatExpiry } from '../utils/format'
import { HOST_STATUS_LABELS } from '../utils/status'

const ENROLL_HELP: readonly HelpItem[] = [
  ['Comando', 'Ejecútalo como administrador (Windows) o root (Linux/macOS). Con servidor https o localhost descarga el agente solo; si no, córrelo en la carpeta donde copiaste el binario (argos-agent.exe). Instala con las opciones marcadas, enrola y arranca el servicio.'],
  ['Token', 'Se muestra solo aquí. Trátalo como una contraseña: quien lo tenga puede registrar un equipo falso.'],
  ['Inventario y Parches', 'Activan esos módulos del agente (puertos, servicios, procesos y actualizaciones pendientes).'],
  ['Sondear dispositivos', 'El agente hace ping/SNMP a dispositivos de las redes permitidas (CIDR).'],
  ['Modo privilegiado', 'Ve procesos, puertos y servicios de otros usuarios. La instalación requiere root o administrador.'],
  ['Permitir http', 'Solo en LAN sin TLS: el agente habla sin cifrado con el servidor. No lo uses por internet.'],
]

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
  const [options, setOptions] = useState<AgentOptions>(DEFAULT_AGENT_OPTIONS)
  const effective = { ...options, insecure: options.insecure && showInsecure }
  const netsResult = options.probe ? parseNets(options.probeNets) : null
  // Con la casilla recién marcada y el campo vacío no se grita: el error aparece al escribir; la copia sigue bloqueada.
  const netsError = netsResult && !netsResult.ok && options.probeNets.trim() !== '' ? netsResult.error : null
  const windows = buildEnrollCommand(token, origin, 'windows', effective)
  const linux = buildEnrollCommand(token, origin, 'linux', effective)

  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-3">
      <DialogHeader className="flex-row items-center gap-1">
        <DialogTitle>{title}</DialogTitle>
        <FormHelp title={title} items={ENROLL_HELP} />
        <span className="ml-auto text-xs text-text-muted tabular-nums" title={`Expira a las ${formatExpiry(token.expires_at)}`}>
          {left > 0 ? `${token.max_uses > 1 ? `${token.max_uses} equipos · ` : ''}expira en ${left} min` : 'Token expirado'}
        </span>
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
        </>
      ) : (
        <div className="flex flex-col items-start gap-2">
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
      <TriangleAlertIcon className="mt-0.5 size-4 shrink-0 text-warn" strokeWidth={1.5} aria-hidden />
      <span>
        Ya existe un host con este nombre ({HOST_STATUS_LABELS[conflict.status]}). Si es el mismo equipo,{' '}
        <Link
          to={`/hosts/${encodeURIComponent(conflict.host_id)}`}
          className="rounded-md text-brand underline-offset-4 outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring"
        >
          reactívalo o re-enrólalo
        </Link>
        .
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
