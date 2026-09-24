export const OS_LABELS: Record<string, string> = { windows: 'Windows', linux: 'Linux', darwin: 'macOS' }

/** Nombre legible del SO: `os_name` sin prefijo "Microsoft " (+ `os_version` si aporta); si no hay, mapea `os`. */
export function formatOs(
  os: string,
  osName?: string | null,
  osVersion?: string | null,
): string {
  const name = osName?.replace(/^Microsoft\s+/i, '').trim()
  if (!name) return OS_LABELS[os.toLowerCase()] ?? os
  const version = osVersion?.trim()
  return version && !name.includes(version) ? `${name} ${version}` : name
}
