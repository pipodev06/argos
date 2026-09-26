import { describe, expect, it } from 'vitest'
import { HOST_EVENT_SOURCE_LABELS } from './format'

// Regresion: el historial de estado se rompia con los eventos `enroll`/`seed` (from_status vacio, origen sin etiqueta).
describe('origenes de eventos de host', () => {
  it('todos los origenes que envia el servidor tienen etiqueta', () => {
    for (const source of ['detector', 'ingest', 'admin', 'group_change', 'enroll', 'seed']) {
      expect(HOST_EVENT_SOURCE_LABELS[source as keyof typeof HOST_EVENT_SOURCE_LABELS]).toBeTruthy()
    }
  })
})
