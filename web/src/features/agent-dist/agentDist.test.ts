import { describe, expect, it } from 'vitest'
import { archLabel, fileKind, sortFiles, type AgentDistFile } from './agentDist'

const f = (name: string, os: string, arch: string): AgentDistFile => ({ name, os, arch, sha256: 'x', size: 1 })

describe('descargas del agente', () => {
  it('ordena Windows, Linux, macOS; x64 antes que ARM; el .deb antes que el binario', () => {
    const out = sortFiles([
      f('argos-agent_darwin_arm64', 'darwin', 'arm64'),
      f('argos-agent_linux_amd64', 'linux', 'amd64'),
      f('argos-agent_1.0.5_amd64.deb', 'linux', 'amd64'),
      f('argos-agent_windows_amd64.exe', 'windows', 'amd64'),
    ]).map((x) => x.name)
    expect(out).toEqual(['argos-agent_windows_amd64.exe', 'argos-agent_1.0.5_amd64.deb', 'argos-agent_linux_amd64', 'argos-agent_darwin_arm64'])
  })
  it('etiquetas legibles', () => {
    expect(archLabel('darwin', 'arm64')).toContain('Apple Silicon')
    expect(archLabel('linux', 'amd64')).toBe('x64 (Intel/AMD)')
    expect(fileKind('a.deb')).toBe('Paquete .deb')
    expect(fileKind('a.exe')).toBe('Ejecutable .exe')
  })
})
