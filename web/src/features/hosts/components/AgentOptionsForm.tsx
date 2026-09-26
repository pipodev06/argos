import { CheckField } from '@/components/ui/check-field'
import { FormField } from '@/components/ui/form-field'
import type { AgentOptions } from '../utils/enroll'

interface AgentOptionsFormProps {
  value: AgentOptions
  onChange: (next: AgentOptions) => void
  /** Error de la lista de redes (null = válida o no aplica). */
  netsError: string | null
  /** El servidor usa http: solo entonces se ofrece `--insecure`. */
  showInsecure: boolean
}

/** Opciones del agente: cada una añade un flag al comando de instalación antes de copiarlo. */
export function AgentOptionsForm({ value, onChange, netsError, showInsecure }: AgentOptionsFormProps) {
  const set = <K extends keyof AgentOptions>(key: K, v: AgentOptions[K]) => onChange({ ...value, [key]: v })
  return (
    <fieldset className="grid gap-2 rounded-md border border-border p-3">
      <legend className="px-1 text-sm font-semibold">Opciones del agente</legend>
      <CheckField id="opt-inventory" label="Inventario (puertos, servicios, procesos)" checked={value.inventory} onChange={(v) => set('inventory', v)} />
      <CheckField id="opt-updates" label="Parches del sistema" checked={value.updates} onChange={(v) => set('updates', v)} />
      <CheckField id="opt-probe" label="Puede sondear dispositivos (ping / SNMP)" checked={value.probe} onChange={(v) => set('probe', v)} />
      {value.probe && (
        <FormField
          id="opt-probe-nets"
          label="Redes permitidas (CIDR, separadas por comas)"
          value={value.probeNets}
          onChange={(e) => set('probeNets', e.target.value)}
          placeholder="192.168.1.0/24, 10.0.0.0/16"
          autoComplete="off"
          spellCheck={false}
          error={netsError}
        />
      )}
      <CheckField
        id="opt-privileged"
        label="Modo privilegiado (más visibilidad)"
        hint="Ve más procesos, puertos y servicios de otros usuarios. Requiere ejecutar la instalación como root o administrador."
        checked={value.privileged}
        onChange={(v) => set('privileged', v)}
      />
      {showInsecure && (
        <CheckField
          id="opt-insecure"
          label="El servidor usa http (solo LAN sin TLS)"
          hint="El agente aceptará hablar sin cifrado con el servidor. No lo uses por internet."
          checked={value.insecure}
          onChange={(v) => set('insecure', v)}
        />
      )}
    </fieldset>
  )
}
