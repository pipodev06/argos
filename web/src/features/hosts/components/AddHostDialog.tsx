import { useState, type FormEvent } from 'react'
import { IconAlertTriangle } from '@tabler/icons-react'
import { Button } from '@/components/ui/button'
import { CopyBlock } from '@/components/ui/copy-block'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { FormField } from '@/components/ui/form-field'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { GroupSelect } from '@/features/groups/components/GroupSelect'
import { useGroups } from '@/features/groups/hooks/useGroups'
import { useNow } from '@/hooks/useNow'
import { AgentOptionsForm } from './AgentOptionsForm'
import { useEnrollHost } from '../hooks/useEnrollHost'
import { applyAgentOptions, DEFAULT_AGENT_OPTIONS, enrollCommands, isHttpOrigin, minutesLeft, parseNets, type AgentOptions } from '../utils/enroll'
import { formatExpiry } from '../utils/format'

interface AddHostDialogProps {
  open: boolean
  onClose: () => void
}

export function AddHostDialog({ open, onClose }: AddHostDialogProps) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      {/* El contenido se monta solo abierto: al cerrar se descartan hostname, token y comandos del estado. */}
      <DialogContent className="sm:max-w-2xl">{open && <AddHostBody onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}

function AddHostBody({ onClose }: { onClose: () => void }) {
  const [hostname, setHostname] = useState('')
  const [groupId, setGroupId] = useState<string | null>(null)
  const { groups } = useGroups()
  const { token, error, submitting, submit, reset } = useEnrollHost()

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const trimmed = hostname.trim()
    if (trimmed) void submit(trimmed, groupId)
  }

  if (token) return <TokenStep token={token} onClose={onClose} onAnother={reset} />

  return (
    <form onSubmit={handleSubmit} className="grid gap-4">
      <DialogHeader>
        <DialogTitle>Agregar host</DialogTitle>
        <DialogDescription>Genera un token para enrolar el agente en este host.</DialogDescription>
      </DialogHeader>
      <FormField
        id="hostname"
        label="Hostname"
        value={hostname}
        onChange={(e) => setHostname(e.target.value)}
        placeholder="srv-web-01"
        autoComplete="off"
        autoFocus
        error={error}
      />
      <div className="grid gap-1.5">
        <label htmlFor="enroll-group" className="text-sm font-medium text-text">
          Grupo
        </label>
        <GroupSelect id="enroll-group" value={groupId} onChange={setGroupId} groups={groups?.items ?? []} />
      </div>
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onClose}>
          Cancelar
        </Button>
        <Button type="submit" disabled={submitting || !hostname.trim()}>
          {submitting ? 'Generando...' : 'Generar token'}
        </Button>
      </DialogFooter>
    </form>
  )
}

interface TokenStepProps {
  token: NonNullable<ReturnType<typeof useEnrollHost>['token']>
  onClose: () => void
  onAnother: () => void
}

function TokenStep({ token, onClose, onAnother }: TokenStepProps) {
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
        <DialogTitle>Instala el agente</DialogTitle>
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

function BlockedCommand() {
  return (
    <p role="status" className="text-sm text-text-muted">
      Corrige las redes permitidas para ver y copiar el comando.
    </p>
  )
}
