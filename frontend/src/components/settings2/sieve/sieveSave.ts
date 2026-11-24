// sieveSave.ts runs a save from the out of office and server filter pages:
// it reports a refused script as a problem to show beside the form, anything
// else as a toast, and hands back the state the server is in afterwards.

import { get } from 'svelte/store'
import { t } from '../../../lib/i18n'
import { toastError, toastSuccess, errorMessage } from '../../../stores/toast'
import type { SieveSave, SieveState } from '../../../lib/types'

/** What a save left behind: the new state, or the server's objection. */
export interface SaveOutcome {
  state: SieveState | null
  problem: string
  line: number
}

/** Runs one save call and sorts its outcome. */
export async function runSave(call: () => Promise<SieveSave>): Promise<SaveOutcome> {
  try {
    const result = await call()
    if (result.problem) {
      return { state: null, problem: result.problem, line: result.line }
    }
    if (result.state.status !== 'ok') {
      toastError(get(t)(`sieve.status.${result.state.status}`))
      return { state: result.state, problem: '', line: 0 }
    }
    toastSuccess(get(t)('sieve.saved'))
    return { state: result.state, problem: '', line: 0 }
  } catch (err) {
    toastError(errorMessage(err))
    return { state: null, problem: '', line: 0 }
  }
}
