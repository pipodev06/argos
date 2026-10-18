import type { HelpItem } from '@/components/ui/form-help'

/**
 * Pasos para monitorear un equipo por SNMP de punta a punta (ayuda del perfil, del dispositivo y del chequeo).
 * argos solo acepta SNMPv3 con autenticacion y cifrado (SHA-256/SHA-512 + AES-128/AES-256) o SNMPv2c.
 */
export const SNMP_HOWTO: readonly HelpItem[] = [
  ['Cómo funciona', 'El equipo a monitorear (switch, router, servidor) no lleva agente: un host con agente, el colector, le hace las consultas SNMP por la red.'],
  ['1. En el equipo', 'Activa su agente SNMP y crea un usuario v3. En Ubuntu: sudo apt install snmpd libsnmp-dev; sudo systemctl stop snmpd; sudo net-snmp-create-v3-user -ro -a SHA-512 -A <clave-auth> -x AES -X <clave-cifrado> argos; en /etc/snmp/snmpd.conf pon "agentaddress udp:161"; sudo systemctl start snmpd.'],
  ['2. Perfil SNMP', 'En Perfiles SNMP: SNMPv3, el mismo usuario, autenticación SHA-512 y cifrado AES-128 con las mismas claves del paso 1.'],
  ['3. Dispositivo', 'Dispositivos → Nuevo: IP del equipo, colector (un host con agente de la misma red), el mismo grupo que el colector y este perfil SNMP.'],
  ['4. Colector', 'Su agente debe poder sondear esa red: argos-agent install --probe-allow 192.168.1.0/24 (como administrador).'],
  ['5. Chequeo', 'Chequeos → Nuevo, tipo SNMP: "Responde" o "Estado de interfaz" con su ifIndex; alcance: el dispositivo. En ~1 min pasa a Activo.'],
  ['ifIndex', 'Número de la interfaz en el equipo. Se ve con: snmpwalk -v3 -l authPriv -u argos -a SHA-512 -A <clave-auth> -x AES -X <clave-cifrado> <IP> IF-MIB::ifDescr (ifDescr.2 = eth0 → ifIndex 2).'],
]
