import { useState } from 'react'
import { testChannel } from '../services/channels.service'
import { describeTestError, describeTestResult } from '../utils/errors'

export interface TestOutcome {
  ok: boolean
  message: string
}

/** Prueba de canal: un envío a la vez; el resultado queda por canal solo en memoria. */
export function useChannelTest() {
  const [testingId, setTestingId] = useState<string | null>(null)
  const [outcomes, setOutcomes] = useState<Record<string, TestOutcome>>({})

  async function test(id: string) {
    setTestingId(id)
    let outcome: TestOutcome
    try {
      const res = await testChannel(id)
      outcome = { ok: res.ok, message: describeTestResult(res) }
    } catch (err) {
      outcome = { ok: false, message: describeTestError(err) }
    }
    setOutcomes((o) => ({ ...o, [id]: outcome }))
    setTestingId(null)
  }

  return { testingId, outcomes, test }
}
