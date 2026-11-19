import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  getSetting: vi.fn(),
  setSetting: vi.fn(),
}))

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  ...api,
}))

import { coachHint, hintMs, initCoach, resetCoachHints } from './coach'
import { prefs } from './prefs'
import { noteAction, noteShortcut, recordInput, hintLimit } from '../lib/coach'

let stop: () => void

// click runs an action the way a mouse does: a pointer press, then the action.
function click(action: Parameters<typeof noteAction>[0]): void {
  recordInput('pointer')
  noteAction(action)
}

function waitOut(): void {
  vi.advanceTimersByTime(hintMs + 1)
}

beforeEach(async () => {
  vi.useFakeTimers()
  api.getSetting.mockResolvedValue({ value: '', found: false })
  api.setSetting.mockResolvedValue(undefined)
  prefs.update((p) => ({ ...p, design: 'new', coachHints: true }))
  stop = await initCoach()
  resetCoachHints()
})

afterEach(() => {
  stop()
  vi.useRealTimers()
})

describe('mouse hints', () => {
  it('names the key after an action done by mouse', () => {
    click('archive')
    expect(get(coachHint)).toMatchObject({ action: 'archive', combo: 'e', labelKey: 'shortcut.archive' })
    waitOut()
    expect(get(coachHint)).toBeNull()
  })

  it('shows each action a few times at most', () => {
    for (let i = 0; i < hintLimit; i++) {
      click('archive')
      expect(get(coachHint)?.action).toBe('archive')
      waitOut()
    }
    click('archive')
    expect(get(coachHint)).toBeNull()
  })

  it('stops for good once the key has been used', () => {
    recordInput('key')
    noteShortcut('archive')
    click('archive')
    expect(get(coachHint)).toBeNull()
  })

  it('never stacks a second hint on the first', () => {
    click('archive')
    click('reply')
    expect(get(coachHint)?.action).toBe('archive')
    waitOut()
    click('reply')
    expect(get(coachHint)?.action).toBe('reply')
  })

  it('stays quiet with hints off or in the classic design', () => {
    prefs.update((p) => ({ ...p, coachHints: false }))
    click('archive')
    expect(get(coachHint)).toBeNull()
    prefs.update((p) => ({ ...p, coachHints: true, design: 'classic' }))
    click('archive')
    expect(get(coachHint)).toBeNull()
  })

  it('keeps its counters in the local settings only', () => {
    click('archive')
    expect(api.setSetting).toHaveBeenLastCalledWith('shortcut_coach', JSON.stringify({ shown: { archive: 1 }, learned: {} }))
  })

  it('starts over after a reset', () => {
    for (let i = 0; i < hintLimit; i++) {
      click('archive')
      waitOut()
    }
    resetCoachHints()
    click('archive')
    expect(get(coachHint)?.action).toBe('archive')
  })
})
