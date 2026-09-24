export const MAX_TAGS = 20
export const MAX_TAG_LENGTH = 32

// Caracteres de control y de direccion bidi (el servidor los rechaza).
// eslint-disable-next-line no-control-regex
const FORBIDDEN = /[\u0000-\u001f\u007f-\u009f‎‏‪-‮⁦-⁩]/

/** Mensaje de error de una etiqueta (ya con trim), o null si es valida (mismas reglas que el servidor). */
export function validateTag(tag: string): string | null {
  if (tag.length < 1 || tag.length > MAX_TAG_LENGTH) return `La etiqueta debe tener entre 1 y ${MAX_TAG_LENGTH} caracteres.`
  if (FORBIDDEN.test(tag)) return 'La etiqueta tiene caracteres no permitidos.'
  return null
}

/** La lista ya tiene `tag` (sin distinguir mayusculas). */
export function hasTag(tags: readonly string[], tag: string): boolean {
  const low = tag.toLowerCase()
  return tags.some((t) => t.toLowerCase() === low)
}

export const TAG_ERRORS: Record<string, string> = {
  too_many_tags: `Máximo ${MAX_TAGS} etiquetas por host.`,
  invalid_tag: `Etiqueta inválida: 1 a ${MAX_TAG_LENGTH} caracteres, sin caracteres de control.`,
}
