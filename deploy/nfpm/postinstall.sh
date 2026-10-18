#!/bin/sh
# No enrola ni arranca el agente: solo explica los pasos. El token se pide en el dashboard (Hosts > Agregar host).
cat <<'MSG'

argos-agent instalado en /usr/local/bin/argos-agent. Falta enrolarlo (como root):

  argos-agent install
  echo '<TOKEN>' | argos-agent enroll --server https://TU-DOMINIO --token-stdin
  argos-agent start

El token sale del dashboard (Hosts > Agregar host). Opciones de install: --inventory all,
--probe-allow <CIDR>, --privileged. Estado: argos-agent status.

MSG
exit 0
