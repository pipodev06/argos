import type { ReactNode } from 'react'
import { CopyBlock } from '@/components/ui/copy-block'
import { PageHeader } from '@/components/ui/page-header'

interface SectionProps {
  title: string
  children: ReactNode
}

/** Sección con separador inferior (sin sombra: borde 1px, como el resto del estilo Slate). */
function Section({ title, children }: SectionProps) {
  return (
    <section className="flex flex-col gap-3 border-b border-border pb-6 last:border-b-0 last:pb-0">
      <h2 className="text-sm font-semibold">{title}</h2>
      <div className="flex flex-col gap-4">{children}</div>
    </section>
  )
}

/** Título corto + comando copiable. Sin texto explicativo: el título ya dice qué es. */
function Cmd({ title, value }: { title: string; value: string }) {
  return (
    <div className="flex flex-col gap-1">
      <p className="text-sm font-medium text-text">{title}</p>
      <CopyBlock label={title} value={value} />
    </div>
  )
}

/**
 * Página de referencia estática (sin llamadas a la API): comandos reales del CLI del server, tal como están
 * documentados en `server/cmd/server/main.go`, `agent/cmd/agent/main.go` y `deploy/install-server.sh`.
 */
export function InstructionsPage() {
  return (
    <>
      <PageHeader title="Instrucciones" />
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <Section title="Instalar el server">
          <Cmd
            title="LAN (HTTPS con CA propia)"
            value={
              'sudo bash deploy/install-server.sh --public-url https://argos.lan \\\n  --install-postgres --create-local-db --with-caddy --tls-internal --admin-user admin'
            }
          />
          <Cmd
            title="Dominio público (Let's Encrypt)"
            value={
              'sudo bash deploy/install-server.sh --domain argos.midominio.com \\\n  --install-postgres --create-local-db --with-caddy --admin-user admin'
            }
          />
          <Cmd
            title="Detrás de un túnel (sin Caddy)"
            value={
              'sudo bash deploy/install-server.sh --public-url https://argos.midominio.com \\\n  --install-postgres --create-local-db --admin-user admin'
            }
          />
          <Cmd
            title="Solo esta máquina (sin dominio)"
            value="sudo bash deploy/install-server.sh --public-url http://127.0.0.1:8080 --install-postgres --create-local-db --admin-user admin"
          />
          <Cmd title="Cambiar de dominio (ya instalado)" value="sudo bash deploy/install-server.sh --domain argos.nuevo-dominio.com --with-caddy" />
          <Cmd title="Revisar sin ejecutar nada" value="bash deploy/install-server.sh --dry-run <mismas opciones>" />
        </Section>

        <Section title="Actualizar el agente">
          <Cmd title="Actualizar a la última versión publicada" value="sudo argos-agent update" />
        </Section>

        <Section title="Administración del server (argos-server)">
          <Cmd title="Crear un usuario admin" value="argos-server create-admin [--must-change] [usuario]" />
          <Cmd title="Fijar contraseña y desbloquear la cuenta" value="argos-server reset-password <usuario>" />
          <Cmd title="Aplicar migraciones pendientes" value="argos-server migrate" />
          <Cmd title="Rotar la clave de cifrado" value="argos-server rotate-secret-key" />
        </Section>
      </div>
    </>
  )
}
