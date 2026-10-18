import type { HelpItem } from '@/components/ui/form-help'

/** Ayuda de los permisos de un usuario (alta y edicion de permisos). */
export const PERMISSIONS_HELP: readonly HelpItem[] = [
  ['Grupos', 'Hosts que el usuario puede ver. Los hosts sin grupo solo los ve quien tiene acceso a todos los grupos.'],
  ['Ver procesos y servicios', 'Dato sensible. Solo aplica a los hosts que el usuario puede ver.'],
  ['Ver parches del SO', 'Dato sensible: revela la exposición de cada host (parches de seguridad pendientes, fin de vida).'],
  ['Reconocer alertas', 'Puede marcar como reconocidas las alertas de los hosts que ve. Resolver sigue siendo solo de administradores.'],
  ['Cambios', 'Se aplican en la próxima petición del usuario.'],
]

/** Ayuda del alta de usuario. */
export const USER_HELP: readonly HelpItem[] = [
  ['Usuario', 'Letras, números y . _ @ + -'],
  ['Contraseña', 'Temporal: el usuario deberá cambiarla al entrar por primera vez.'],
  ...PERMISSIONS_HELP,
]
