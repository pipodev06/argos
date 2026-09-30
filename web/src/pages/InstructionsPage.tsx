import { IconAlertTriangle } from '@tabler/icons-react'
import type { ReactNode } from 'react'
import { CopyBlock } from '@/components/ui/copy-block'
import { PageHeader } from '@/components/ui/page-header'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

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

function Note({ children }: { children: ReactNode }) {
  return (
    <p className="flex items-start gap-1.5 text-sm text-text-muted">
      <IconAlertTriangle className="mt-0.5 size-4 shrink-0 text-warn" stroke={1.5} aria-hidden />
      <span>{children}</span>
    </p>
  )
}

/**
 * Página de referencia estática (sin llamadas a la API): comandos reales del CLI del agente y del server, tal
 * como están documentados en `agent/cmd/agent/main.go`, `server/cmd/server/main.go` y `deploy/install-server.sh`.
 * Nada de esto es dinámico ni lleva secretos: el token de enrolamiento real se genera en el diálogo "Agregar
 * host" (Hosts) o "Re-enrolar" (detalle de host), nunca aquí.
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

        <Section title="Enrolar un host (dar de alta un agente)">
          <p className="text-sm text-text-muted">
            Normal: botón <strong>Agregar host</strong> (Hosts) o <strong>Re-enrolar</strong> (detalle de host).
          </p>
          <Tabs defaultValue="windows">
            <TabsList aria-label="Sistema operativo">
              <TabsTrigger value="windows">Windows (PowerShell)</TabsTrigger>
              <TabsTrigger value="linux">Linux / macOS (bash)</TabsTrigger>
            </TabsList>
            <TabsContent value="windows">
              <CopyBlock
                label="Instalar y enrolar en Windows"
                value={
                  "'<token>' | argos-agent.exe enroll --server https://argos.tu-dominio --token-stdin\nargos-agent.exe install\nargos-agent.exe start"
                }
              />
            </TabsContent>
            <TabsContent value="linux">
              <CopyBlock
                label="Instalar y enrolar en Linux o macOS"
                value={
                  "echo '<token>' | argos-agent enroll --server https://argos.tu-dominio --token-stdin\nsudo argos-agent install\nsudo argos-agent start"
                }
              />
            </TabsContent>
          </Tabs>
          <p className="text-sm text-text-muted">
            Flags de <code>install</code>: <code>--privileged</code>, <code>--inventory &lt;módulos|all&gt;</code>,{' '}
            <code>--probe-allow &lt;cidr&gt;</code>, <code>--ca-file ca.pem</code>, <code>--insecure</code>.
          </p>
        </Section>

        <Section title="Actualizar el agente">
          <Cmd title="Actualizar a la última versión publicada" value="sudo argos-agent update" />
        </Section>

        <Section title="Renovar el token de un host">
          <p className="text-sm text-text-muted">
            Detalle del host → <strong>Re-enrolar</strong> (o <strong>Reactivar</strong> si está deshabilitado):
            token nuevo, el anterior queda invalidado, el historial se conserva.
          </p>
        </Section>

        <Section title="Eliminar (dar de baja)">
          <p className="text-sm text-text-muted">
            Host, dispositivo o cliente (grupo): en su propia pantalla, confirmando el nombre. Corre en segundo
            plano con progreso; puedes cerrar la ventana.
          </p>
          <Note>Irreversible. Solo se recupera con un respaldo de la base de datos (rotan a los 14 días).</Note>
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
