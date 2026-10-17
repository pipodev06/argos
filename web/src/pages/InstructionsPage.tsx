import { IconChevronDown } from '@tabler/icons-react'
import type { ReactNode } from 'react'
import { CopyBlock } from '@/components/ui/copy-block'
import { PageHeader } from '@/components/ui/page-header'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

/** Sección plegable (`<details>` nativo: accesible, sin JS propio). Cerrada por defecto. */
function Collapsible({ title, children }: { title: string; children: ReactNode }) {
  return (
    <details className="group rounded-md border border-border">
      <summary className="flex cursor-pointer list-none items-center justify-between gap-2 rounded-md px-4 py-3 text-sm font-semibold outline-none [&::-webkit-details-marker]:hidden focus-visible:ring-2 focus-visible:ring-ring">
        {title}
        <IconChevronDown className="size-4 shrink-0 text-text-muted transition-transform group-open:rotate-180" stroke={1.5} aria-hidden />
      </summary>
      <div className="flex flex-col gap-4 border-t border-border px-4 py-4">{children}</div>
    </details>
  )
}

/** Subtítulo dentro de una sección plegable (agrupa comandos sin abrir otro nivel de plegado). */
function SubHeading({ children }: { children: ReactNode }) {
  return <p className="text-sm font-semibold text-text first:pt-0 [&:not(:first-child)]:pt-2">{children}</p>
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
 * Página de referencia estática (sin llamadas a la API): comandos reales del CLI del server y del agente, tal
 * como están documentados en `server/cmd/server/main.go`, `agent/cmd/agent/main.go` y `deploy/install-server.sh`.
 * El agente compila para 3 SO (windows/linux/darwin, `scripts/build-agent.sh`); el server (`deploy/install-server.sh`)
 * solo instala en Linux con systemd, no hay instalador para Windows/macOS.
 */
export function InstructionsPage() {
  return (
    <>
      <PageHeader title="Instrucciones" />
      <div className="flex flex-col gap-3">
        <Collapsible title="Levantarlo en tu máquina de desarrollo (scripts/run-local.sh)">
          <SubHeading>Windows (PowerShell, con el alias de $PROFILE)</SubHeading>
          <Cmd title="Arrancar (compila y levanta)" value="argos-local start" />
          <Cmd title="Ver estado" value="argos-local status" />
          <Cmd title="Seguir el log" value="argos-local logs" />
          <Cmd title="Detener" value="argos-local stop" />

          <SubHeading>Windows (PowerShell, sin el alias) / Linux o macOS (bash)</SubHeading>
          <Cmd title="Arrancar (compila y levanta)" value="bash scripts/run-local.sh start" />
          <Cmd title="Detener" value="bash scripts/run-local.sh stop" />
        </Collapsible>

        <Collapsible title="Generar el paquete de instalación">
          <Cmd title="Compilar el agente (windows/linux/macOS)" value="bash scripts/build-agent.sh 1.0.0" />
          <Cmd title="Compilar server + web y armar el .tar.gz" value="bash scripts/package-server.sh 1.0.0 --build-web" />
        </Collapsible>

        <Collapsible title="Instalación">
          <SubHeading>Servidor (Linux — único SO soportado hoy)</SubHeading>
          <Cmd
            title="Extraer el paquete"
            value="tar -xzf argos-server_1.0.0_linux_amd64.tar.gz && cd argos-server_1.0.0_linux_amd64"
          />
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

          <SubHeading>Agente (Windows, Linux y macOS)</SubHeading>
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
          <Cmd title="Actualizar el agente a la última versión publicada" value="sudo argos-agent update" />
        </Collapsible>

        <Collapsible title="Administración del servidor">
          <Cmd title="Crear un usuario admin" value="argos-server create-admin [--must-change] [usuario]" />
          <Cmd title="Fijar contraseña y desbloquear la cuenta" value="argos-server reset-password <usuario>" />
          <Cmd title="Aplicar migraciones pendientes" value="argos-server migrate" />
          <Cmd title="Rotar la clave de cifrado" value="argos-server rotate-secret-key" />
        </Collapsible>
      </div>
    </>
  )
}
