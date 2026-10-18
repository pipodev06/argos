import { describe, expect, it } from 'vitest'
import { applyAgentOptions, buildEnrollCommand, DEFAULT_AGENT_OPTIONS, isHttpOrigin, isValidCidr, parseBulkUses, parseNets, type AgentOptions } from './enroll'
import type { EnrollToken } from '../types/host.types'

describe('alta masiva: parseBulkUses', () => {
  it('acepta enteros entre 2 y 500', () => {
    expect(parseBulkUses('50')).toEqual({ ok: true, uses: 50 })
    expect(parseBulkUses('2')).toEqual({ ok: true, uses: 2 })
    expect(parseBulkUses('500')).toEqual({ ok: true, uses: 500 })
  })
  it('rechaza fuera de rango, vacío o no entero', () => {
    expect(parseBulkUses('1').ok).toBe(false)
    expect(parseBulkUses('501').ok).toBe(false)
    expect(parseBulkUses('').ok).toBe(false)
    expect(parseBulkUses('3.5').ok).toBe(false)
    expect(parseBulkUses('abc').ok).toBe(false)
  })
})

const LINUX = 'curl -fsSL https://argos.lan/install.sh | sh -s -- --server https://argos.lan --token T0K'
const WIN = `powershell -NoProfile -Command "iwr https://argos.lan/install.ps1 | iex; Install -Server 'https://argos.lan' -Token 'T0K'"`
const OFF: AgentOptions = { inventory: false, updates: false, probe: false, probeNets: '', privileged: false, insecure: false }
const apply = (os: 'linux' | 'windows', o: Partial<AgentOptions>) => applyAgentOptions(os === 'linux' ? LINUX : WIN, os, { ...OFF, ...o })

describe('applyAgentOptions', () => {
  it('sin opciones el comando queda intacto', () => {
    expect(apply('linux', {})).toEqual({ ok: true, command: LINUX })
    expect(apply('windows', {})).toEqual({ ok: true, command: WIN })
  })
  it('por defecto: inventario completo y parches', () => {
    const r = applyAgentOptions(LINUX, 'linux', DEFAULT_AGENT_OPTIONS)
    expect(r).toEqual({ ok: true, command: `${LINUX} --inventory ports,services,processes,updates` })
  })
  it('solo lo seleccionado en --inventory', () => {
    expect(apply('linux', { updates: true })).toEqual({ ok: true, command: `${LINUX} --inventory updates` })
    expect(apply('linux', { inventory: true })).toEqual({ ok: true, command: `${LINUX} --inventory ports,services,processes` })
  })
  it('Linux: todo junto, en orden y al final', () => {
    const r = apply('linux', { inventory: true, updates: true, probe: true, probeNets: '192.168.1.0/24', privileged: true, insecure: true })
    expect(r).toEqual({
      ok: true,
      command: `${LINUX} --inventory ports,services,processes,updates --probe-allow 192.168.1.0/24 --privileged --insecure`,
    })
  })
  it('Windows: inserta antes de la comilla final, con comillas simples', () => {
    const r = apply('windows', { updates: true, probe: true, probeNets: '192.168.1.0/24, fd00::/8', privileged: true, insecure: true })
    expect(r).toEqual({
      ok: true,
      command: WIN.slice(0, -1) + ` -Inventory 'updates' -ProbeAllow '192.168.1.0/24,fd00::/8' -Privileged -Insecure"`,
    })
  })
  it('Windows sin comilla final: se deja intacto (nunca se rompe el comando)', () => {
    expect(applyAgentOptions('x -Server y', 'windows', { ...OFF, privileged: true })).toEqual({ ok: true, command: 'x -Server y' })
  })
  it('CIDR inválido o vacío con sondeo bloquea la copia; sin sondeo se ignora el texto', () => {
    expect(apply('linux', { probe: true, probeNets: '10.0.0.0/33' }).ok).toBe(false)
    expect(apply('windows', { probe: true, probeNets: '' }).ok).toBe(false)
    expect(apply('linux', { probe: false, probeNets: 'basura; rm -rf /' })).toEqual({ ok: true, command: LINUX })
  })
  it('nunca inyecta texto libre', () => {
    for (const evil of ["10.0.0.0/8'; calc; '", '10.0.0.0/8" ; x', '10.0.0.0/8 --privileged', '$(id)/8', '10.0.0.0/8\n;id']) {
      expect(apply('linux', { probe: true, probeNets: evil }).ok).toBe(false)
      expect(apply('windows', { probe: true, probeNets: evil }).ok).toBe(false)
    }
  })
})

describe('CIDR', () => {
  it('válidos IPv4 e IPv6', () => {
    for (const c of ['192.168.1.0/24', '10.0.0.0/8', '1.2.3.4/32', 'fd00::/8', '2001:db8::/32', 'fe80::1/128', '2001:db8:0:0:0:0:0:1/64', '::1/128']) {
      expect(isValidCidr(c), c).toBe(true)
    }
  })
  it('inválidos: sin prefijo, fuera de rango, octetos, /0, basura', () => {
    for (const c of ['192.168.1.0', '192.168.1.0/33', '256.1.1.1/24', '1.2.3/24', '1.2.3.4.5/24', '0.0.0.0/0', '::/0', 'fd00::/129', 'fd00:::1/64', '1::2::3/64', 'g::1/64', '12345::/16', '1:2:3:4:5:6:7:8:9/64', '/24', 'a.b.c.d/8', '10.0.0.0/-1', '10.0.0.0/8/8', ' ']) {
      expect(isValidCidr(c), c).toBe(false)
    }
  })
  it('parseNets: lista, espacios, repetidas y mensajes', () => {
    expect(parseNets(' 10.0.0.0/8 , 10.0.0.0/8,,192.168.0.0/16 ')).toEqual({ ok: true, nets: ['10.0.0.0/8', '192.168.0.0/16'] })
    expect(parseNets('')).toMatchObject({ ok: false })
    expect(parseNets('0.0.0.0/0')).toMatchObject({ ok: false, error: expect.stringContaining('/0') })
    expect(parseNets('10.0.0.0/8, nope')).toMatchObject({ ok: false, error: expect.stringContaining('nope') })
    expect(parseNets(Array.from({ length: 33 }, (_, i) => `10.0.${i}.0/24`).join(','))).toMatchObject({ ok: false })
  })
  it('origen http', () => {
    expect(isHttpOrigin('http://192.168.1.5:8080')).toBe(true)
    expect(isHttpOrigin('https://argos.lan')).toBe(false)
  })
})

describe('buildEnrollCommand (sin instalador del servidor)', () => {
  const token = { enroll_token: 'abc123', expires_at: '2026-10-18T00:00:00Z', max_uses: 1 } as unknown as EnrollToken
  const opts: AgentOptions = { inventory: true, updates: true, probe: true, probeNets: '192.168.100.0/24', privileged: true, insecure: true }

  it('Windows: install con todas las opciones, luego enroll y start', () => {
    const r = buildEnrollCommand(token, 'http://192.168.100.18:8080', 'windows', opts)
    expect(r.ok && r.command.split('\n')).toEqual([
      "$a = '.\\argos-agent.exe'",
      '& $a install --inventory ports,services,processes,updates --probe-allow 192.168.100.0/24 --privileged --insecure',
      "'abc123' | & $a enroll --server http://192.168.100.18:8080 --token-stdin",
      '& $a start',
    ])
  })

  it('Linux: mismos pasos con sudo; red invalida bloquea la copia', () => {
    const r = buildEnrollCommand(token, 'http://x:8080', 'linux', { ...opts, privileged: false, insecure: false })
    expect(r.ok && r.command.split('\n')[0]).toBe('sudo ./argos-agent install --inventory ports,services,processes,updates --probe-allow 192.168.100.0/24')
    expect(buildEnrollCommand(token, 'http://x:8080', 'linux', { ...opts, probeNets: 'nope' }).ok).toBe(false)
  })
})
