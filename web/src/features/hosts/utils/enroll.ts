import type { EnrollCommands, EnrollToken } from '../types/host.types'

/** Minutos que faltan para `expiresAt` (redondeado hacia arriba); 0 = expiro. */
export function minutesLeft(expiresAt: string, now: number): number {
  const ms = new Date(expiresAt).getTime() - now
  return ms <= 0 ? 0 : Math.ceil(ms / 60_000)
}

/**
 * Comandos por SO. Usa los que devuelve el servidor (`commands`); si aun no vienen, construye el del agente
 * (`argos-agent enroll --server <origen> --token-stdin`) en el cliente, con el token por stdin.
 */
export function enrollCommands(token: EnrollToken, origin: string): Required<EnrollCommands> {
  const quoted = `'${token.enroll_token}'`
  return {
    windows:
      token.commands?.windows ??
      `${quoted} | .\\argos-agent.exe enroll --server ${origin} --token-stdin`,
    linux:
      token.commands?.linux ??
      `echo ${quoted} | argos-agent enroll --server ${origin} --token-stdin`,
  }
}
