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

import { activeTip, dismissTip, initFirstTips, noteTipContext, resetTips, setTipsSuspended } from './firsttips'
import { coachHint, coachQuiet, initCoach, resetCoachHints } from './coach'
import { prefs } from './prefs'
import { noteAction, recordInput } from '../lib/coach'

let stopCoach: () => void

beforeEach(async () => {
  vi.useFakeTimers()
  api.getSetting.mockResolvedValue({ value: '', found: false })
  api.setSetting.mockResolvedValue(undefined)
  prefs.update((p) => ({ ...p, design: 'new', showTips: true, coachHints: true }))
  stopCoach = await initCoach()
  resetCoachHints()
  await initFirstTips()
  setTipsSuspended(false)
  resetTips()
})

afterEach(() => {
  dismissTip()
  stopCoach()
  vi.useRealTimers()
})

function lastSaved(): string[] {
  const calls = api.setSetting.mock.calls.filter((c) => c[0] === 'tips_seen')
  return JSON.parse(calls[calls.length - 1][1] as string) as string[]
}

describe('tip scheduler', () => {
  it('shows a tip and stores it as seen at once', () => {
    expect(noteTipContext('mail-drag')).toBe(true)
    expect(get(activeTip)?.id).toBe('mail-drag')
    expect(lastSaved()).toEqual(['mail-drag'])
  })

  it('keeps to one tip at a time', () => {
    noteTipContext('mail-drag')
    resetTipsKeepingActive()
    expect(noteTipContext('notes-links')).toBe(false)
    expect(get(activeTip)?.id).toBe('mail-drag')
  })

  it('shows one new tip per session', () => {
    noteTipContext('mail-drag')
    dismissTip()
    expect(noteTipContext('notes-links')).toBe(false)
    expect(get(activeTip)).toBeNull()
  })

  it('never shows a dismissed tip again', () => {
    noteTipContext('mail-drag')
    dismissTip()
    // a new session reads the stored list back.
    api.getSetting.mockResolvedValue({ value: '["mail-drag"]', found: true })
    return initFirstTips().then(() => {
      expect(noteTipContext('mail-drag')).toBe(false)
    })
  })

  it('stays quiet when switched off or suspended', () => {
    prefs.update((p) => ({ ...p, showTips: false }))
    expect(noteTipContext('mail-drag')).toBe(false)
    prefs.update((p) => ({ ...p, showTips: true }))
    setTipsSuspended(true)
    expect(noteTipContext('mail-drag')).toBe(false)
    setTipsSuspended(false)
    expect(noteTipContext('mail-drag')).toBe(true)
  })

  it('stays out of the classic design', () => {
    prefs.update((p) => ({ ...p, design: 'classic' }))
    expect(noteTipContext('mail-drag')).toBe(false)
  })

  it('lets every tip show again after a reset', () => {
    noteTipContext('mail-drag')
    dismissTip()
    resetTips()
    expect(noteTipContext('mail-drag')).toBe(true)
  })
})

describe('with the shortcut coach', () => {
  it('holds coach hints back while a tip shows, and lets them through after', () => {
    noteTipContext('mail-drag')
    expect(get(coachQuiet)).toBe(true)
    recordInput('pointer')
    noteAction('flag')
    expect(get(coachHint)).toBeNull()
    dismissTip()
    expect(get(coachQuiet)).toBe(false)
    recordInput('pointer')
    noteAction('flag')
    expect(get(coachHint)?.action).toBe('flag')
  })

  it('waits while a coach hint shows', () => {
    recordInput('pointer')
    noteAction('flag')
    expect(get(coachHint)).not.toBeNull()
    expect(noteTipContext('mail-drag')).toBe(false)
  })

  it('does not name the same keys twice: the coach drops the hint a tip gave', () => {
    expect(noteTipContext('archive-undo')).toBe(true)
    dismissTip()
    recordInput('pointer')
    noteAction('undo')
    expect(get(coachHint)).toBeNull()
  })
})

// resetTips clears the session and the active tip; this restores the active
// one so the busy rule can be seen on its own.
function resetTipsKeepingActive(): void {
  const tip = get(activeTip)
  resetTips()
  activeTip.set(tip)
}
