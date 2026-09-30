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
      <div className="flex flex-col gap-3 text-sm text-text-muted">{children}</div>
    </section>
  )
}

function Note({ children }: { children: ReactNode }) {
  return (
    <p className="flex items-start gap-1.5 text-sm">
      <IconAlertTriangle className="mt-0.5 size-4 shrink-0 text-warn" stroke={1.5} aria-hidden />
      <span>{children}</span>
    </p>
  )
}

/**
 * Página de referencia estática (sin llamadas a la API): comandos reales del CLI del agente y del server, tal
 * como están documentados en `agent/cmd/agent/main.go` y `server/cmd/server/main.go`. Nada de esto es dinámico
 * ni lleva secretos: el token de enrolamiento real se genera en el diálogo "Agregar host" (Hosts) o "Re-enrolar"
 * (detalle de host), nunca aquí.
 */
export function InstructionsPage() {
  return (
    <>
      <PageHeader title="Instrucciones" />
      <div className="flex max-w-3xl flex-col gap-6">
        <Section title="Instalar el server">
          <p>
            Un Linux limpio (Debian 12+/Ubuntu 22.04+) con acceso root y el paquete descomprimido
            (<code>argos-server_&lt;version&gt;_linux_&lt;amd64|arm64&gt;.tar.gz</code>, generado con{' '}
            <code>scripts/package-server.sh</code>). Un solo comando instala Postgres 18, crea la base de datos,
            instala Caddy con HTTPS, arranca el server y crea el primer admin:
          </p>
          <CopyBlock
            label="Instalar el server (LAN, sin dominio público)"
            value={
              'sudo bash deploy/install-server.sh --public-url https://argos.lan \\\n  --install-postgres --create-local-db --with-caddy --tls-internal --admin-user admin'
            }
          />
          <p>
            Con dominio público (Let&apos;s Encrypt automático, puertos 80 y 443 abiertos): cambia{' '}
            <code>--public-url ... --tls-internal</code> por <code>--domain argos.midominio.com</code>. Detrás de
            un túnel (Cloudflare Tunnel u otro, sin abrir puertos): igual pero sin <code>--with-caddy</code>.
          </p>
          <CopyBlock
            label="Revisar antes de instalar, sin ejecutar nada"
            value="bash deploy/install-server.sh --dry-run <las mismas opciones>"
          />
          <p>
            La contraseña del primer admin se imprime <strong>una sola vez</strong> al final; no queda guardada en
            ningún archivo ni log. Reinstalar (con o sin opciones) es idempotente: no duplica ni rompe nada.
          </p>
        </Section>

        <Section title="Enrolar un host (dar de alta un agente)">
          <p>
            Lo normal es el botón <strong>Agregar host</strong> (en Hosts) o <strong>Re-enrolar / Reactivar</strong>{' '}
            (en el detalle de un host): arman el comando ya con el token y el servidor correctos, listo para pegar
            en la máquina nueva. El token dura unos minutos y se usa una sola vez (o varias, si es de alta masiva).
          </p>
          <p>Si prefieres escribirlo a mano, la forma general es:</p>
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
          <p>
            Opciones útiles de <code>install</code>: <code>--privileged</code> (más visibilidad, opcional),{' '}
            <code>--inventory &lt;módulos|all&gt;</code> (puertos/servicios/procesos/parches),{' '}
            <code>--probe-allow &lt;cidr,...&gt;</code> (rangos que puede sondear como colector de dispositivos sin
            agente), <code>--ca-file ca.pem</code> (CA interna, solo con <code>--tls-internal</code>) e{' '}
            <code>--insecure</code> (solo pruebas locales por HTTP, nunca en producción). El modal "Agregar host"
            ya las arma con casillas.
          </p>
        </Section>

        <Section title="Actualizar el agente">
          <CopyBlock label="Actualizar el agente a la última versión publicada" value="sudo argos-agent update" />
          <p>
            Requiere administrador/root. Descarga la versión que el server tiene publicada y la verifica por firma
            antes de reemplazar el binario. También puedes correr el instalador con <code>--update</code>.
          </p>
        </Section>

        <Section title="Renovar el token de un host">
          <p>
            Un host no "renueva" su token solo: si sospechas que se filtró, si vas a reinstalar en otra máquina, o
            si el host quedó deshabilitado, usa el botón <strong>Re-enrolar</strong> (o <strong>Reactivar</strong>{' '}
            si está deshabilitado) en el detalle del host. Genera un token nuevo, invalida el anterior de
            inmediato y conserva todo el historial del host.
          </p>
        </Section>

        <Section title="Eliminar (dar de baja)">
          <p>
            Host, dispositivo o cliente completo (grupo) se eliminan desde su propia pantalla, con confirmación
            escribiendo el nombre. La baja corre en segundo plano en el server: puedes cerrar la ventana y seguirá
            hasta terminar; el diálogo muestra el progreso. Al terminar una baja de cliente, puedes descargar una
            constancia con los conteos (sin nombres de hosts).
          </p>
          <Note>
            Es irreversible: borra las métricas, el historial y la configuración propia de ese host o grupo. Los
            respaldos de la base de datos (rotan a los 14 días) son la única forma de recuperarlo.
          </Note>
        </Section>

        <Section title="Comandos de administración del server (argos-server)">
          <CopyBlock label="Crear un usuario admin" value="argos-server create-admin [--must-change] [usuario]" />
          <CopyBlock label="Fijar una contraseña y desbloquear la cuenta" value="argos-server reset-password <usuario>" />
          <CopyBlock label="Aplicar migraciones pendientes" value="argos-server migrate" />
          <CopyBlock
            label="Rotar la clave de cifrado (tras definir ARGOS_SECRET_KEY_PREVIOUS)"
            value="argos-server rotate-secret-key"
          />
          <p>
            Se corren en la máquina del server, con las variables de entorno ya definidas (<code>DATABASE_URL</code>
            , etc). <code>keygen</code>, <code>pubkey</code> y <code>sign-manifest</code> (firma de binarios del
            agente) son solo para la máquina de compilación: la clave privada nunca debe copiarse al server
            desplegado.
          </p>
        </Section>

        <Section title="Variables de entorno importantes">
          <ul className="flex flex-col gap-1">
            <li>
              <code>ARGOS_PUBLIC_URL</code> — URL pública exacta del server (alimenta la cookie de sesión, CSP y
              los comandos de instalación).
            </li>
            <li>
              <code>ARGOS_TRUSTED_PROXIES</code> — IP del proxy o túnel; sin definirla detrás de un proxy, todos
              los clientes comparten IP para el límite de intentos.
            </li>
            <li>
              <code>ARGOS_SECRET_KEY</code> / <code>ARGOS_SECRET_KEY_PREVIOUS</code> — cifra secretos (canales,
              perfiles SNMP); la segunda solo mientras dura una rotación.
            </li>
            <li>
              <code>ARGOS_METRICS_TOKEN</code> — habilita <code>GET /metrics</code> (Prometheus); sin definirla la
              ruta responde 404. Mínimo 32 caracteres.
            </li>
            <li>
              <code>ARGOS_AUTO_MIGRATE</code> — aplica migraciones al arrancar (por defecto activado).
            </li>
          </ul>
        </Section>
      </div>
    </>
  )
}
