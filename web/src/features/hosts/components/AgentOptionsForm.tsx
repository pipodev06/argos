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
      <CheckField id="opt-inventory" label="Inventario" checked={value.inventory} onChange={(v) => set('inventory', v)} />
      <CheckField id="opt-updates" label="Parches del sistema" checked={value.updates} onChange={(v) => set('updates', v)} />
      <CheckField id="opt-probe" label="Sondear dispositivos" checked={value.probe} onChange={(v) => set('probe', v)} />
      {value.probe && (
        <FormField
          id="opt-probe-nets"
          label="Redes permitidas"
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
        label="Modo privilegiado"
        checked={value.privileged}
        onChange={(v) => set('privileged', v)}
      />
      {showInsecure && (
        <CheckField
          id="opt-insecure"
          label="Permitir http (sin TLS)"
          checked={value.insecure}
          onChange={(v) => set('insecure', v)}
        />
      )}
    </fieldset>
  )
}
