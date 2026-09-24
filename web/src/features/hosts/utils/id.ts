const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

/** Formato UUID (el mismo que valida el server: si no coincide, ni se consulta la API). */
export function isHostId(value: string): boolean {
  return UUID.test(value)
}
