#!/usr/bin/env bash
# Prueba de punta a punta (humo) de cada apartado de la API contra un server real, un agente real y una BD
# desechable "argos_e2e" (DROP/CREATE). NUNCA toca "argos" ni "argos_perf". Uso:
#   bash scripts/e2e/smoke.sh [--keep] [--agent-secs N]
#     --keep         no borra la BD argos_e2e ni el directorio de trabajo al terminar
#     --agent-secs   segundos extra que corre el agente real antes de los chequeos (por defecto 45)
# Requiere: go, curl, python y psql de PostgreSQL 18 (PGBIN) con el usuario argos/argos. Sale con 1 si algo FALLA.
set -uo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
KEEP=0 AGENT_SECS=45
while [[ $# -gt 0 ]]; do
	case "$1" in
	--keep) KEEP=1 ;;
	--agent-secs) AGENT_SECS="$2"; shift ;;
	*) echo "argumento desconocido: $1" >&2; exit 2 ;;
	esac
	shift
done

PGBIN="${PGBIN:-/c/Program Files/PostgreSQL/18/bin}"
export PGUSER="${PGUSER:-argos}" PGPASSWORD="${PGPASSWORD:-argos}" PGHOST="${PGHOST:-localhost}"
DB=argos_e2e
PORT=18090
BASE="http://127.0.0.1:$PORT"
W="${E2E_DIR:-${TMPDIR:-/tmp}/argos-e2e}"
ADMIN_PASS="E2e-Admin-2026-xyz!"
VIEWER_PASS="E2e-Viewer-2026-abc!"
VIEWER_PASS2="E2e-Viewer-2026-new!"
EXE=""; [[ "$(uname -s)" == MINGW* || "$(uname -s)" == MSYS* ]] && EXE=".exe"

rm -rf "$W"; mkdir -p "$W/state" "$W/dist"
server_bin="$W/argos-server$EXE"; agent_bin="$W/argos-agent$EXE"
server_pid="" agent_pid="" lis_pid=""

# ---------- utilidades ----------
declare -A SEC_FAIL SEC_PASS
declare -a SEC_ORDER
CUR=""
section() { CUR="$1"; SEC_ORDER+=("$1"); SEC_FAIL[$1]=""; SEC_PASS[$1]=0; echo "== $1"; }
ok() { SEC_PASS[$CUR]=$((SEC_PASS[$CUR] + 1)); }
bad() { SEC_FAIL[$CUR]+="${SEC_FAIL[$CUR]:+; }$1"; echo "   FALLA: $1"; }
# expect_code ESPERADO DESCRIPCION: compara con $CODE de la ultima llamada.
expect_code() { if [[ "$CODE" == "$1" ]]; then ok; else bad "$2: esperaba $1, llego $CODE ($(head -c 160 "$W/body" 2>/dev/null | tr '\n' ' '))"; fi; }
# one_of "A B" DESCRIPCION: $CODE debe ser uno de los valores.
one_of() { if [[ " $1 " == *" $CODE "* ]]; then ok; else bad "$2: esperaba $1, llego $CODE ($(head -c 160 "$W/body" 2>/dev/null | tr '\n' ' '))"; fi; }
# expect_json DESCRIPCION EXPR_PYTHON: evalua sobre `d` (JSON del ultimo body); debe dar True.
expect_json() {
	local r; r="$(jx "$2" 2>&1)"
	if [[ "$r" == "True" ]]; then ok; else bad "$1 ($2 -> ${r:0:100})"; fi
}
jx() { python -I -c 'import sys,json
d=json.load(open(sys.argv[1],encoding="utf-8"))
if isinstance(d,dict) and "error" in d and "data" in d: d=d["data"]
print(eval(sys.argv[2]))' "$W/body" "$1"; }
jv() { jx "$1" 2>/dev/null; }

JAR="$W/admin.jar"
# Expresion que saca el id de la respuesta ({"id":..} o {"group":{"id":..}}).
JID="(d['id'] if 'id' in d else [v for v in d.values() if isinstance(v,dict) and 'id' in v][0]['id'])"
# api METODO RUTA [JSON] -> CODE y $W/body. Usa la jar actual ($JAR).
api() {
	local m="$1" p="$2" b="${3:-}"
	local args=(-s -o "$W/body" -w '%{http_code}' -X "$m" -b "$JAR" -c "$JAR" --max-time 30)
	[[ -n "$b" ]] && args+=(-H 'Content-Type: application/json' --data-binary "$b")
	CODE="$(curl "${args[@]}" "$BASE$p")" || CODE=000
}

psql() { "$PGBIN/psql" -v ON_ERROR_STOP=1 "$@"; }
winkill() { local pid="$1"; [[ -z "$pid" ]] && return; local wp; wp="$(cat /proc/$pid/winpid 2>/dev/null)"; kill "$pid" 2>/dev/null; [[ -n "$wp" ]] && taskkill //F //PID "$wp" >/dev/null 2>&1; true; }
cleanup() {
	winkill "$agent_pid"; winkill "$server_pid"; winkill "${lis_pid:-}"
	sleep 1
	if [[ $KEEP -eq 0 ]]; then
		psql -q -d postgres -c "DROP DATABASE IF EXISTS $DB WITH (FORCE)" >/dev/null 2>&1
		rm -rf "$W"
	else
		echo "(--keep) BD $DB y $W conservados"
	fi
}
trap cleanup EXIT

# ---------- build + entorno ----------
section "00 build y arranque"
(cd "$root/server" && go build -o "$server_bin" ./cmd/server) && ok || bad "compilar server"
(cd "$root/agent" && go build -o "$agent_bin" ./cmd/agent) && ok || bad "compilar agente"
psql -q -d postgres -c "DROP DATABASE IF EXISTS $DB WITH (FORCE)" -c "CREATE DATABASE $DB" >/dev/null && ok || bad "crear BD $DB"
cred="$PGUSER"; cred+=":$PGPASSWORD" # credenciales solo de variables, nunca literales en una URL
export DATABASE_URL="postgres://${cred}@$PGHOST:5432/$DB?sslmode=disable"
export ARGOS_LISTEN_ADDR="127.0.0.1:$PORT" ARGOS_PUBLIC_URL="$BASE" ARGOS_INSECURE_COOKIES=true
export ARGOS_SECRET_KEY="$(head -c 32 /dev/zero | base64)" ARGOS_METRICS_TOKEN="e2e-metrics-token-0123456789abcdef"
[[ -f "$root/web/dist/index.html" ]] && export ARGOS_WEB_DIR="$root/web/dist"
export ARGOS_AGENT_DIST_DIR="$W/dist"
[[ -d "$root/dist/agent" ]] && export ARGOS_AGENT_DIST_DIR="$root/dist/agent"
timeout 90 "$server_bin" migrate >"$W/migrate.log" 2>&1 && ok || bad "migrate (ver $W/migrate.log)"
printf '%s\n' "$ADMIN_PASS" | "$server_bin" create-admin admin >"$W/admin.log" 2>&1 && ok || bad "create-admin: $(tail -c 200 "$W/admin.log")"
"$server_bin" >"$W/server.log" 2>&1 &
server_pid=$!
up=0; for _ in $(seq 1 40); do curl -sf "$BASE/health" >/dev/null && { up=1; break; }; sleep 0.5; done
[[ $up -eq 1 ]] && ok || { bad "el server no arranco (ver $W/server.log)"; tail -5 "$W/server.log"; }

# ---------- auth ----------
section "01 auth"
api POST /api/v1/auth/login '{"username":"admin","password":"mala"}'; expect_code 401 "login con clave mala"
api GET /api/v1/hosts; expect_code 401 "hosts sin sesion"
api POST /api/v1/auth/login "{\"username\":\"admin\",\"password\":\"$ADMIN_PASS\"}"; expect_code 200 "login admin"
grep -q argos_sid "$JAR" && ok || bad "no hay cookie de sesion"
api GET /api/v1/auth/me; expect_code 200 "me"
expect_json "me.role=admin" "d.get('role')=='admin' or d.get('user',{}).get('role')=='admin'"
CODE="$(curl -s -o "$W/body" -w '%{http_code}' -X POST -b "$JAR" -H 'Origin: http://evil.example' -H 'Content-Type: application/json' -d '{"name":"x"}' "$BASE/api/v1/groups")"
expect_code 403 "POST con Origin ajeno"
CODE="$(curl -s -o "$W/body" -w '%{http_code}' -H 'Host: evil.example' -b "$JAR" "$BASE/api/v1/summary")"
expect_code 421 "Host no permitido"
api GET /health; expect_code 200 "health"
CODE="$(curl -s -o "$W/body" -w '%{http_code}' "$BASE/metrics")"; one_of "401 403 404" "metrics sin token"
CODE="$(curl -s -o "$W/body" -w '%{http_code}' -H "Authorization: Bearer $ARGOS_METRICS_TOKEN" "$BASE/metrics")"; expect_code 200 "metrics con token"

# ---------- grupos ----------
section "02 grupos"
api POST /api/v1/groups '{"name":"e2e-grupo","always_on":false}'; expect_code 201 "crear grupo"
GID="$(jv "$JID")"
[[ -n "$GID" ]] && ok || bad "grupo sin id"
api POST /api/v1/groups '{"name":"e2e-grupo"}'; expect_code 409 "grupo duplicado"
api POST /api/v1/groups '{"name":""}'; expect_code 400 "grupo sin nombre"
api PATCH "/api/v1/groups/$GID" '{"name":"e2e-grupo-ed","down_after_seconds":120}'; expect_code 200 "editar grupo"
api GET /api/v1/groups; expect_code 200 "listar grupos"
expect_json "lista trae el grupo editado" "any(g['name']=='e2e-grupo-ed' for g in (d if isinstance(d,list) else d.get('items',d.get('groups',[]))))"
api GET "/api/v1/groups/$GID/usage"; expect_code 200 "uso del grupo"
api POST /api/v1/groups '{"name":"e2e-borrar"}'; G2="$(jv "$JID")"
api DELETE "/api/v1/groups/$G2"; one_of "200 204" "borrar grupo vacio"
api DELETE "/api/v1/groups/00000000-0000-4000-8000-000000000000"; expect_code 404 "borrar grupo inexistente"

# ---------- hosts + agente real ----------
section "03 hosts y enroll"
api POST /api/v1/hosts/enroll-token "{\"hostname\":\"e2e-host\",\"group_id\":\"$GID\"}"; expect_code 201 "enroll-token"
TOKEN="$(jv "d.get('token') or d.get('enroll_token')")"
[[ -n "$TOKEN" && "$TOKEN" != None ]] && ok || bad "enroll-token sin token en la respuesta"
api POST /api/v1/hosts/enroll-token '{"hostname":""}'; expect_code 400 "enroll-token sin hostname"
export ARGOS_STATE_DIR="$W/state" ARGOS_ALLOW_INSECURE=1
printf '%s\n' "$TOKEN" | "$agent_bin" enroll --server "$BASE" --token-stdin >"$W/enroll.log" 2>&1 && ok || bad "agent enroll: $(tail -c 200 "$W/enroll.log")"
printf '%s\n' "$TOKEN" | "$agent_bin" enroll --server "$BASE" --token-stdin --force >"$W/enroll2.log" 2>&1 && bad "reusar el token debio fallar" || ok
# El agente no sondea loopback: se usa la IP de LAN de esta maquina con un listener TCP de prueba.
LANIP="$(python -I -c 'import socket;s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);s.connect(("192.0.2.1",9));print(s.getsockname()[0])' 2>/dev/null)"
[[ -z "$LANIP" ]] && LANIP=192.0.2.99
python -I -m http.server 18091 --bind "$LANIP" >/dev/null 2>&1 &
lis_pid=$!
ARGOS_SERVER_URL="$BASE" ARGOS_INTERVAL=10s ARGOS_PROBE_ALLOW=192.0.2.10/32,$LANIP/32 ARGOS_INVENTORY_MODULES=ports,services,processes,updates \
	"$agent_bin" run >"$W/agent.log" 2>&1 &
agent_pid=$!
HID=""
for _ in $(seq 1 30); do
	api GET "/api/v1/hosts?q=e2e-host"
	HID="$(jv "(d['items'][0]['id'] if d['items'] else '')")"; [[ -n "$HID" ]] && break; sleep 1
done
[[ -n "$HID" ]] && ok || bad "el host no aparece tras enrolar"
for _ in $(seq 1 40); do api GET "/api/v1/hosts/$HID"; [[ "$(jv "d['host']['status']")" == up ]] && break; sleep 1; done
expect_json "host pasa a up" "d['host']['status']=='up'"
api GET /api/v1/hosts; expect_code 200 "lista hosts"
expect_json "lista con items/total" "'items' in d and 'total' in d"
api GET "/api/v1/hosts?limit=1&sort=cpu&dir=desc"; expect_code 200 "lista hosts con sort/limit"
api GET "/api/v1/hosts?limit=999"; one_of "200 400" "limit gigante"
api GET "/api/v1/hosts?sort=;drop"; one_of "200 400" "sort invalido"
api GET "/api/v1/hosts/not-a-uuid"; one_of "400 404" "id invalido"
api GET "/api/v1/hosts/$HID"; expect_code 200 "detalle host"
expect_json "detalle: grupo asignado" "(d['host']['group'] or {}).get('id')=='$GID'"
api PUT "/api/v1/hosts/$HID/name" '{"hostname":"e2e-host-ren"}'; expect_code 200 "renombrar host"
api GET "/api/v1/hosts/$HID"; expect_json "nombre aplicado" "d['host']['hostname']=='e2e-host-ren'"
api PUT "/api/v1/hosts/$HID/group" '{"group_id":null}'; expect_code 200 "quitar grupo"
api PUT "/api/v1/hosts/$HID/group" "{\"group_id\":\"$GID\"}"; expect_code 200 "volver a poner grupo"
api PUT "/api/v1/hosts/$HID/group" '{}'; expect_code 400 "group_id requerido"
api PUT "/api/v1/hosts/$HID/retention" '{"detail_retention_days":30}'; expect_code 200 "retencion del host"
api PUT "/api/v1/hosts/$HID/retention" '{"detail_retention_days":2}'; expect_code 400 "retencion fuera de rango"
api PUT "/api/v1/hosts/$HID/retention" '{"detail_retention_days":null}'; expect_code 200 "retencion heredada"
api GET /api/v1/hosts.csv; expect_code 200 "hosts.csv"
api GET /api/v1/summary; expect_code 200 "summary"
expect_json "summary con contenido" "isinstance(d,dict) and len(d)>0"
api GET "/api/v1/hosts/$HID/events"; expect_code 200 "eventos del host"

section "04 metricas e inventario (agente)"
sleep "$((AGENT_SECS > 25 ? 15 : 5))"
FROM="$(date -u -d '-1 hour' +%Y-%m-%dT%H:%M:%SZ)"; TO="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
api GET "/api/v1/hosts/$HID/metrics?from=$FROM&to=$TO"; expect_code 200 "metricas del host"
expect_json "hay puntos de metricas" "len(json.dumps(d))>40"
api GET "/api/v1/hosts/$HID/history?from=$FROM&to=$TO"; expect_code 200 "historial del host"
for m in ports services processes changes; do
	api GET "/api/v1/hosts/$HID/inventory/$m"; one_of "200 404" "inventory/$m"
done
api GET "/api/v1/hosts/$HID/inventory/ports"; expect_json "puertos con contenido" "len(json.dumps(d))>5"
api GET "/api/v1/hosts/$HID/history?from=$TO&to=$FROM"; expect_code 400 "historial con rango invertido"

section "05 parches"
api GET "/api/v1/hosts/$HID/updates"; one_of "200 404" "hosts/id/updates"
api GET "/api/v1/updates"; expect_code 200 "updates de la flota"
api GET "/api/v1/updates?history=zzz"; one_of "200 400" "updates?history invalido"

section "06 dispositivos"
NOID="00000000-0000-4000-8000-000000000000"
api POST /api/v1/devices "{\"name\":\"e2e-dev\",\"address\":\"192.0.2.10\",\"collector_id\":\"$HID\",\"group_id\":\"$GID\"}"; expect_code 201 "crear dispositivo"
DID="$(jv "$JID")"; [[ -n "$DID" ]] && ok || bad "dispositivo sin id"
api POST /api/v1/devices "{\"name\":\"e2e-dev2\",\"address\":\"192.0.2.10\",\"collector_id\":\"$HID\",\"group_id\":\"$GID\"}"; expect_code 409 "dispositivo duplicado"
api POST /api/v1/devices "{\"name\":\"x\",\"address\":\"no es ip!\",\"collector_id\":\"$HID\"}"; expect_code 400 "direccion invalida"
api POST /api/v1/devices "{\"name\":\"x\",\"address\":\"10.0.0.9\",\"collector_id\":\"$NOID\"}"; one_of "422 404" "colector inexistente"
api GET "/api/v1/devices?collector=$HID"; expect_code 200 "filtro collector"
expect_json "filtro collector trae el dispositivo" "len(d['items'])==1"
api GET "/api/v1/devices?collector=$NOID"; expect_json "collector ajeno = vacio" "len(d['items'])==0"
api GET "/api/v1/devices/$DID"; expect_code 200 "detalle dispositivo"
api PATCH "/api/v1/devices/$DID" '{"name":"e2e-dev-ren"}'; expect_code 200 "renombrar dispositivo"
api PATCH "/api/v1/devices/$DID" '{"enabled":false}'; expect_code 200 "deshabilitar"
api PATCH "/api/v1/devices/$DID" '{"enabled":true}'; expect_code 200 "habilitar"
api DELETE "/api/v1/groups/$GID"; expect_code 409 "no borra grupo con hosts"

section "07 perfiles SNMP"
api POST /api/v1/snmp-profiles '{"name":"e2e-snmp","version":2,"allow_v2c":true,"community":"publico-e2e"}'; expect_code 201 "crear perfil v2c"
SPID="$(jv "$JID")"
expect_json "no devuelve la comunidad" "'publico-e2e' not in json.dumps(d)"
api POST /api/v1/snmp-profiles '{"name":"e2e-snmp","version":2,"allow_v2c":true,"community":"x"}'; expect_code 409 "perfil duplicado"
api POST /api/v1/snmp-profiles '{"name":"malo","version":3}'; expect_code 400 "perfil v3 incompleto"
api GET /api/v1/snmp-profiles; expect_code 200 "listar perfiles"
api GET "/api/v1/snmp-profiles/$SPID"; expect_code 200 "detalle perfil"
expect_json "detalle sin secretos" "'publico-e2e' not in json.dumps(d)"
api PATCH "/api/v1/snmp-profiles/$SPID" '{"name":"e2e-snmp-ed"}'; expect_code 200 "editar perfil"
api PATCH "/api/v1/devices/$DID" "{\"snmp_profile_id\":\"$SPID\"}"; expect_code 200 "asignar perfil a dispositivo"
api DELETE "/api/v1/snmp-profiles/$SPID"; expect_code 409 "perfil en uso"
api PATCH "/api/v1/devices/$DID" '{"snmp_profile_id":null}'; expect_code 200 "quitar perfil"
api DELETE "/api/v1/snmp-profiles/$SPID"; one_of "200 204" "borrar perfil"

section "08 chequeos"
TCP="\"type\":\"tcp\",\"interval_seconds\":30,\"timeout_ms\":2000,\"retries\":0,\"target_host\":\"$LANIP\""
HS="\"scope\":{\"type\":\"host\",\"host_id\":\"$HID\"}"
api POST /api/v1/service-checks "{\"name\":\"e2e-tcp\",$TCP,\"target_port\":18091,$HS}"; expect_code 201 "crear chequeo tcp"
CID="$(jv "$JID")"
api POST /api/v1/service-checks "{\"name\":\"e2e-tcp\",$TCP,\"target_port\":18091,$HS}"; expect_code 409 "chequeo duplicado"
api POST /api/v1/service-checks "{\"name\":\"e2e-tcp-malo\",$TCP,$HS}"; expect_code 400 "tcp sin puerto"
api POST /api/v1/service-checks "{\"name\":\"e2e-ping\",\"type\":\"ping\",\"interval_seconds\":30,\"timeout_ms\":2000,\"retries\":0,\"scope\":{\"type\":\"device\",\"device_id\":\"$DID\"}}"; expect_code 201 "crear chequeo ping a dispositivo"
PCID="$(jv "$JID")"
api POST /api/v1/service-checks "{\"name\":\"e2e-tcp-cerrado\",$TCP,\"target_port\":1,$HS}"; expect_code 201 "crear tcp a puerto cerrado"
BCID="$(jv "$JID")"
api PATCH "/api/v1/service-checks/$CID" '{"interval_seconds":60}'; expect_code 200 "editar chequeo"
echo "   (esperando 40 s a que el agente ejecute los chequeos)"; sleep 50
api GET /api/v1/service-checks; expect_code 200 "listar chequeos"
expect_json "failing_hosts presente" "all('failing_hosts' in c for c in d['items'])"
expect_json "el tcp cerrado falla en 1 host" "[c for c in d['items'] if c['id']=='$BCID'][0]['failing_hosts']>=1"
expect_json "el tcp al server no falla" "[c for c in d['items'] if c['id']=='$CID'][0]['failing_hosts']==0"
api GET "/api/v1/service-checks/$CID"; expect_code 200 "detalle chequeo"
api GET "/api/v1/hosts/$HID/service-checks"; expect_code 200 "chequeos del host"
api GET "/api/v1/hosts/$HID/service-checks/$CID/events"; expect_code 200 "eventos del chequeo"
api GET "/api/v1/service-checks?cursor=basura"; one_of "200 400" "cursor invalido"

section "09 canales"
api POST /api/v1/channels '{"name":"e2e-hook","kind":"webhook","url":"https://hooks.example.invalid/x","min_severity":"warn"}'; one_of "201 400 422" "crear canal"
CHID="$(jv "$JID")"
api POST /api/v1/channels '{"name":"e2e-http","kind":"webhook","url":"http://hooks.example.com/x"}'; expect_code 400 "webhook http (no https) rechazado"
api POST /api/v1/channels '{"name":"e2e-loop","kind":"webhook","url":"https://127.0.0.1/x"}'; one_of "400 422" "webhook a loopback (SSRF)"
api POST /api/v1/channels '{"name":"e2e-bad","kind":"sms","url":"https://hooks.example.com/x"}'; expect_code 400 "kind invalido"
api GET /api/v1/channels; expect_code 200 "listar canales"
expect_json "canales sin hmac_secret" "'hmac_secret' not in json.dumps(d)"
if [[ -n "$CHID" ]]; then
	api PATCH "/api/v1/channels/$CHID" '{"min_severity":"critical"}'; expect_code 200 "editar canal"
	api GET "/api/v1/channels/$CHID"; expect_code 200 "detalle canal"
fi

section "10 alertas y reglas"
api POST /api/v1/alert-rules "{\"name\":\"e2e-cpu\",\"kind\":\"metric\",\"metric\":\"cpu_pct\",\"op\":\"gt\",\"threshold\":0,\"duration_seconds\":0,\"severity\":\"warn\",\"scope\":{\"type\":\"host\",\"host_id\":\"$HID\"}}"
expect_code 201 "crear regla metric"
RID="$(jv "$JID")"
api POST /api/v1/alert-rules "{\"name\":\"e2e-svc\",\"kind\":\"service_check\",\"check_id\":\"$BCID\",\"duration_seconds\":0,\"severity\":\"critical\",\"scope\":{\"type\":\"all\"}}"
expect_code 201 "crear regla service_check"
RID2="$(jv "$JID")"
api POST /api/v1/alert-rules '{"name":"e2e-cpu","kind":"metric","metric":"cpu_pct","op":"gt","threshold":1,"duration_seconds":0,"severity":"warn","scope":{"type":"all"}}'; expect_code 409 "regla duplicada"
api POST /api/v1/alert-rules '{"name":"e2e-bad","kind":"metric","metric":"nada","op":"gt","threshold":1,"duration_seconds":0,"severity":"warn","scope":{"type":"all"}}'; expect_code 400 "metrica invalida"
if [[ -n "$RID" ]]; then
	api PATCH "/api/v1/alert-rules/$RID" '{"threshold":99,"severity":"info"}'; expect_code 200 "editar regla"
	api PATCH "/api/v1/alert-rules/$RID" '{"threshold":0,"severity":"warn"}'; expect_code 200 "regla de vuelta a 0"
	[[ -n "$CHID" ]] && { api PUT "/api/v1/alert-rules/$RID/channels" "{\"channel_ids\":[\"$CHID\"]}"; expect_code 200 "canales de la regla"; }
	api GET "/api/v1/alert-rules/$RID"; expect_code 200 "detalle regla"
fi
api GET /api/v1/alert-rules; expect_code 200 "listar reglas"
echo "   (esperando alertas)"
for _ in $(seq 1 40); do api GET "/api/v1/alerts?state=open"; [[ "$(jv "len(d['items'])")" -ge 1 ]] 2>/dev/null && break; sleep 2; done
expect_code 200 "listar alertas abiertas"
AID="$(jv "d['items'][0]['id'] if d['items'] else ''")"
[[ -n "$AID" ]] && ok || bad "no se abrio ninguna alerta (regla con umbral 0 / chequeo caido)"
api GET "/api/v1/alerts?q=e2e"; expect_code 200 "alertas con q"
F2="$(date -u -d '-1 day' +%Y-%m-%dT%H:%M:%SZ)"; T2="$(date -u -d '+1 day' +%Y-%m-%dT%H:%M:%SZ)"
api GET "/api/v1/alerts?from=$F2&to=$T2"; expect_code 200 "alertas con from/to"
api GET "/api/v1/alerts?from=ayer"; expect_code 400 "from invalido"
api GET /api/v1/alerts/summary; expect_code 200 "resumen de alertas"
if [[ -n "$AID" ]]; then
	api GET "/api/v1/alerts/$AID"; expect_code 200 "detalle alerta"
	api POST "/api/v1/alerts/$AID/ack" '{}'; one_of "200 204" "ack"
	api POST "/api/v1/alerts/$AID/resolve" '{}'; one_of "200 204" "resolve"
	api POST "/api/v1/alerts/$AID/resolve" '{}'; expect_code 409 "resolver dos veces"
fi
api GET "/api/v1/reports/alerts.csv?from=$F2&to=$T2"; expect_code 200 "alerts.csv"
if [[ -n "$RID2" ]]; then api DELETE "/api/v1/alert-rules/$RID2"; one_of "200 204" "borrar regla svc"; fi
if [[ -n "$RID" ]]; then
	api DELETE "/api/v1/alert-rules/$RID"; one_of "200 204" "borrar regla cpu"
	api GET "/api/v1/alert-rules/$RID"; expect_code 404 "regla borrada = 404"
fi
if [[ -n "$CHID" ]]; then api DELETE "/api/v1/channels/$CHID"; one_of "200 204" "borrar canal"; fi

section "11 mantenimiento"
S="$(date -u -d '+1 hour' +%Y-%m-%dT%H:%M:%SZ)"; E="$(date -u -d '+2 hour' +%Y-%m-%dT%H:%M:%SZ)"
api POST /api/v1/maintenance "{\"name\":\"e2e-mant\",\"starts_at\":\"$S\",\"ends_at\":\"$E\",\"scope\":{\"type\":\"group\",\"group_id\":\"$GID\"}}"; expect_code 201 "crear ventana"
MID="$(jv "$JID")"
api POST /api/v1/maintenance "{\"name\":\"e2e-mala\",\"starts_at\":\"$E\",\"ends_at\":\"$S\",\"scope\":{\"type\":\"all\"}}"; expect_code 400 "ventana con fin antes de inicio"
api POST /api/v1/maintenance "{\"name\":\"e2e-sinscope\",\"starts_at\":\"$S\",\"ends_at\":\"$E\"}"; expect_code 400 "ventana sin scope"
api GET /api/v1/maintenance; expect_code 200 "listar ventanas"
api GET "/api/v1/maintenance/$MID"; expect_code 200 "detalle ventana"
api PATCH "/api/v1/maintenance/$MID" '{"name":"e2e-mant-ed"}'; expect_code 200 "editar ventana"
api DELETE "/api/v1/maintenance/$MID"; one_of "200 204" "borrar ventana"

section "12 informes"
api GET "/api/v1/reports/availability?range=7d"; expect_code 200 "availability 7d"
api GET "/api/v1/reports/availability?range=1d"; expect_code 400 "range invalido"
api GET "/api/v1/reports/service-checks?range=30d"; expect_code 200 "service-checks 30d"
api GET "/api/v1/reports/availability.csv?range=7d"; expect_code 200 "availability.csv"
api GET "/api/v1/reports/service-checks.csv?range=7d"; expect_code 200 "service-checks.csv"
FUT="$(date -u -d '+3 day' +%Y-%m-%dT%H:%M:%SZ)"; F1="$(date -u -d '-2 day' +%Y-%m-%dT%H:%M:%SZ)"
api GET "/api/v1/reports/availability?from=$F1&to=$FUT"; expect_code 200 "rango personalizado con to futuro"
api GET "/api/v1/reports/availability?from=$FUT&to=$FUT"; expect_code 400 "rango vacio"
api GET "/api/v1/reports/availability?month=$(date -u +%Y-%m)"; expect_code 200 "informe por mes"
api GET "/api/v1/reports/availability?range=7d&from=$F1&to=$FUT"; expect_code 400 "range + from/to"
api GET "/api/v1/reports/availability?group_id=$GID&range=7d"; expect_code 200 "informe por grupo"
api GET "/api/v1/reports/availability.csv?from=$F1&to=$FUT"; expect_code 200 "csv con to futuro"

section "13 usuarios y permisos"
api POST /api/v1/users "{\"username\":\"e2e-viewer\",\"password\":\"$VIEWER_PASS\",\"role\":\"viewer\",\"group_ids\":[\"$GID\"]}"; expect_code 201 "crear viewer"
UID_V="$(jv "$JID")"
api POST /api/v1/users "{\"username\":\"e2e-viewer\",\"password\":\"$VIEWER_PASS\",\"role\":\"viewer\"}"; expect_code 409 "usuario duplicado"
api POST /api/v1/users '{"username":"e2e-debil","password":"1234","role":"viewer"}'; expect_code 400 "clave debil"
api GET /api/v1/users; expect_code 200 "listar usuarios"
expect_json "usuarios sin hash" "'hash' not in json.dumps(d).lower()"
api PATCH "/api/v1/users/$UID_V" '{"can_ack":true,"can_inventory":true}'; expect_code 200 "editar permisos"
api PATCH "/api/v1/users/$UID_V" "{\"all_groups\":true,\"group_ids\":[\"$GID\"]}"; expect_code 400 "group_ids con all_groups"
api PATCH "/api/v1/users/$UID_V" "{\"all_groups\":false,\"group_ids\":[\"$GID\"]}"; expect_code 200 "scope por grupo"
api GET /api/v1/auth/me; ME_ID="$(jv "d.get('id') or d['user']['id']")"
api DELETE "/api/v1/users/$ME_ID"; expect_code 409 "no se borra a si mismo"
ADMJ="$JAR"; JAR="$W/viewer.jar"
api POST /api/v1/auth/login "{\"username\":\"e2e-viewer\",\"password\":\"$VIEWER_PASS\"}"; expect_code 200 "login viewer"
api GET /api/v1/hosts; one_of "403 428" "viewer con clave pendiente de cambio no opera"
api POST /api/v1/auth/password "{\"current_password\":\"$VIEWER_PASS\",\"new_password\":\"$VIEWER_PASS2\"}"; one_of "200 204" "cambiar clave viewer"
api POST /api/v1/auth/login "{\"username\":\"e2e-viewer\",\"password\":\"$VIEWER_PASS2\"}"; expect_code 200 "re-login viewer"
api GET /api/v1/hosts; expect_code 200 "viewer lista hosts"
expect_json "viewer ve su host" "d['total']>=1"
api GET "/api/v1/hosts/$HID"; expect_code 200 "viewer detalle host de su grupo"
for p in /api/v1/users /api/v1/audit /api/v1/snmp-profiles /api/v1/settings/retention /api/v1/system/status /api/v1/purge-jobs; do
	api GET "$p"; one_of "403 404" "viewer GET $p"
done
api POST /api/v1/groups '{"name":"viewer-no"}'; one_of "403 404" "viewer POST groups"
api PUT "/api/v1/hosts/$HID/name" '{"hostname":"x"}'; one_of "403 404" "viewer renombra host"
api GET "/api/v1/hosts/$NOID"; expect_code 404 "viewer: host inexistente 404"
api POST /api/v1/auth/logout; one_of "200 204" "logout viewer"
api GET /api/v1/hosts; expect_code 401 "sesion cerrada"
JAR="$ADMJ"
api PATCH "/api/v1/users/$UID_V" '{"disabled":true}'; expect_code 200 "deshabilitar viewer"
api DELETE "/api/v1/users/$UID_V"; one_of "200 204" "borrar viewer"

section "14 auditoria"
api GET /api/v1/audit; expect_code 200 "listar auditoria"
expect_json "auditoria con eventos" "len(d['items'])>0"
expect_json "campos target_name/target_exists" "all('target_exists' in i and 'target_name' in i for i in d['items'])"
api GET "/api/v1/audit?q=e2e-borrar"; expect_code 200 "auditoria con q"
expect_json "q encuentra eventos" "len(d['items'])>0"
expect_json "grupo borrado: target_exists=false" "any(i.get('target_name')=='e2e-borrar' and i['target_exists'] is False for i in d['items'])"
api GET "/api/v1/audit?limit=2"; expect_json "paginacion limit" "len(d['items'])<=2"
api GET "/api/v1/audit?from=$F2&to=$T2"; expect_code 200 "auditoria con from/to"
api GET /api/v1/audit/events; expect_code 200 "catalogo de eventos"
api GET "/api/v1/audit?cursor=basura"; expect_code 400 "cursor invalido"
api GET "/api/v1/audit?q=%27%20OR%201=1--"; expect_code 200 "q con SQL no rompe"

section "15 sistema y retencion"
api GET /api/v1/system/status; expect_code 200 "system/status"
api GET /api/v1/settings/retention; expect_code 200 "leer retencion"
api PUT /api/v1/settings/retention '{"detail_days":60}'; expect_code 200 "fijar retencion 60"
api PUT /api/v1/settings/retention '{"detail_days":1}'; expect_code 400 "retencion fuera de rango"
api GET /api/v1/purge-jobs; expect_code 200 "purge-jobs"

section "16 baja y export"
api GET "/api/v1/groups/$GID/export.zip"; expect_code 200 "export.zip del grupo"
api POST "/api/v1/hosts/$HID/revoke" '{}'; one_of "200 204 409" "revocar host"
winkill "$agent_pid"; agent_pid=""
api DELETE "/api/v1/devices/$DID"; one_of "200 202 204" "borrar dispositivo"

section "17 web estatica"
if [[ -n "${ARGOS_WEB_DIR:-}" ]]; then
	CODE="$(curl -s -o "$W/body" -w '%{http_code}' "$BASE/")"; expect_code 200 "GET /"
	grep -qi '<div id="root"' "$W/body" && ok || bad "/ no es index.html"
	CODE="$(curl -s -o "$W/body2" -w '%{http_code}' "$BASE/hosts/x/metricas")"; expect_code 200 "ruta SPA /hosts/x/metricas"
	cmp -s "$W/body" "$W/body2" && ok || bad "ruta SPA no devuelve index.html"
	CODE="$(curl -s -o "$W/body" -w '%{http_code}' "$BASE/assets/no-existe.js")"; expect_code 404 "asset inexistente"
	CODE="$(curl -s -o "$W/body" -w '%{http_code}' "$BASE/api/v1/no-existe")"; one_of "404 401" "api inexistente"
	curl -s -D "$W/hdr" -o /dev/null "$BASE/"; grep -qi 'content-security-policy' "$W/hdr" && ok || bad "falta CSP en /"
else
	echo "   web/dist no existe: se omite"; ok
fi

# ---------- resumen ----------
echo; echo "================ RESUMEN ================"
fails=0
for s in "${SEC_ORDER[@]}"; do
	if [[ -z "${SEC_FAIL[$s]}" ]]; then printf 'PASA   %-34s (%d checks)\n' "$s" "${SEC_PASS[$s]}"
	else printf 'FALLA  %-34s %s\n' "$s" "${SEC_FAIL[$s]}"; fails=$((fails + 1)); fi
done
echo "errores en server.log: $(grep -ci 'level=ERROR\|"level":"ERROR"' "$W/server.log" 2>/dev/null)"
grep -i 'level=ERROR\|"level":"ERROR"' "$W/server.log" 2>/dev/null | head -5 | cut -c1-300
[[ $fails -eq 0 ]]
