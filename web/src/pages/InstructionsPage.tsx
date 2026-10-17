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
 * Página de referencia estática (sin llamadas a la API), solo para desarrollo: comandos reales de
 * `scripts/run-local.sh`, `scripts/build-agent.sh` y del CLI del server y del agente
 * (`server/cmd/server/main.go`, `agent/cmd/agent/main.go`).
 */
export function InstructionsPage() {
  return (
    <>
      <PageHeader title="Instrucciones" />
      <div className="flex flex-col gap-3">
        <Collapsible title="Levantar el server (Go: compila y sirve API + web en :8080)">
          <SubHeading>Windows (PowerShell, con el alias de $PROFILE)</SubHeading>
          <Cmd title="Arrancar (recompila web y server)" value="argos-local start --web" />
          <Cmd title="Ver estado" value="argos-local status" />
          <Cmd title="Seguir el log" value="argos-local logs" />
          <Cmd title="Detener" value="argos-local stop" />

          <SubHeading>Sin el alias (Git Bash, Linux o macOS)</SubHeading>
          <Cmd title="Arrancar (recompila web y server)" value="bash scripts/run-local.sh start --web" />
          <Cmd title="Detener" value="bash scripts/run-local.sh stop" />
        </Collapsible>

        <Collapsible title="Editar la interfaz con recarga en caliente (Node, :5173)">
          <Cmd title="Con el server ya corriendo, en otra terminal" value="cd web && npm run dev" />
        </Collapsible>

        <Collapsible title="Agente">
          <Cmd title="Compilar el agente (windows/linux/macOS → dist/agent)" value="bash scripts/build-agent.sh 0.0.0-dev" />
          <Cmd title="Otro equipo de la LAN" value="Hosts → Agregar host → marcar insecure → copiar el comando" />
          <SubHeading>Este mismo PC (consola como administrador, desde la raíz del repo; token de Hosts → Agregar host)</SubHeading>
          <Tabs defaultValue="windows">
            <TabsList aria-label="Sistema operativo">
              <TabsTrigger value="windows">Windows (PowerShell)</TabsTrigger>
              <TabsTrigger value="linux">Linux / macOS (bash)</TabsTrigger>
            </TabsList>
            <TabsContent value="windows">
              <CopyBlock
                label="Enrolar e instalar en Windows"
                value={
                  "$a = '.\\dist\\agent\\0.0.0-dev\\argos-agent_windows_amd64.exe'\n'<token>' | & $a enroll --server http://localhost:8080 --token-stdin\n& $a install\n& $a start"
                }
              />
            </TabsContent>
            <TabsContent value="linux">
              <CopyBlock
                label="Enrolar e instalar en Linux o macOS"
                value={
                  "a=./dist/agent/0.0.0-dev/argos-agent_linux_amd64\necho '<token>' | sudo $a enroll --server http://localhost:8080 --token-stdin\nsudo $a install\nsudo $a start"
                }
              />
            </TabsContent>
          </Tabs>
        </Collapsible>

        <Collapsible title="Administración del server">
          <SubHeading>Desde server/, con DATABASE_URL y ARGOS_SECRET_KEY definidas (ver README)</SubHeading>
          <Cmd title="Crear un usuario admin" value="go run ./cmd/server create-admin [--must-change] [usuario]" />
          <Cmd title="Fijar contraseña y desbloquear la cuenta" value="go run ./cmd/server reset-password <usuario>" />
        </Collapsible>
      </div>
    </>
  )
}
