// coach.ts (store) runs the shortcut coach of the new design: the hint that
// names an action's key after it was done by mouse, and whether the sheet of
// shortcuts is showing. The counters behind the hints are kept in the local
// settings table and nowhere else.

import { get, writable } from 'svelte/store'
import { getSetting, setSetting, SettingKeys } from '../lib/api'
import {
  emptyCounters,
  hintAllowed,
  onCoachAction,
  parseCounters,
  trackInput,
  withLearned,
  withShown,
  type CoachCounters,
  type CoachOrigin,
} from '../lib/coach'
import { shortcuts as registry, type ShortcutAction } from '../lib/shortcuts'
import { bindings } from './shortcuts'
import { prefs } from './prefs'

/** The hint on screen: an action and the combo that does it. */
export interface CoachHint {
  id: number
  action: ShortcutAction
  labelKey: string
  combo: string
}

/** The hint showing, or null. */
export const coachHint = writable<CoachHint | null>(null)

/**
 * The shortcut sheet: closed, shown while the modifier is held, or pinned
 * open by its shortcut with a filter field.
 */
export const sheetMode = writable<'closed' | 'hold' | 'pinned'>('closed')

/** How long a hint stays. */
export const hintMs = 4000

let counters: CoachCounters = emptyCounters()
let nextId = 1
let hideTimer: ReturnType<typeof setTimeout> | undefined

function save(): void {
  void setSetting(SettingKeys.coachCounters, JSON.stringify(counters)).catch(() => {})
}

function hide(id: number): void {
  coachHint.update((h) => (h && h.id === id ? null : h))
}

// handle reacts to one action: a shortcut marks it learned, a pointer action
// may bring up its hint. One hint at a time: while one shows, others wait out.
function handle(action: ShortcutAction, origin: CoachOrigin): void {
  if (origin === 'shortcut') {
    if (!counters.learned[action]) {
      counters = withLearned(counters, action)
      save()
    }
    return
  }
  const p = get(prefs)
  if (!p.coachHints || p.design === 'classic' || get(coachHint) !== null) {
    return
  }
  const combo = (get(bindings) as Record<string, string>)[action] ?? ''
  if (!hintAllowed(counters, action, combo)) {
    return
  }
  counters = withShown(counters, action)
  save()
  const id = nextId++
  const labelKey = registry.find((s) => s.action === action)?.labelKey ?? action
  coachHint.set({ id, action, labelKey, combo })
  clearTimeout(hideTimer)
  hideTimer = setTimeout(() => hide(id), hintMs)
}

/**
 * Loads the counters and starts listening for actions and input. Returns the
 * function that stops it.
 */
export async function initCoach(): Promise<() => void> {
  const stopTrack = trackInput()
  const stopBus = onCoachAction(handle)
  try {
    const { value, found } = await getSetting(SettingKeys.coachCounters)
    if (found && value) {
      counters = parseCounters(value)
    }
  } catch {
    // keep the empty counters.
  }
  return () => {
    stopTrack()
    stopBus()
    clearTimeout(hideTimer)
  }
}

/** Forgets every hint shown and key learned, so the hints start over. */
export function resetCoachHints(): void {
  counters = emptyCounters()
  coachHint.set(null)
  save()
}

/** Opens the sheet pinned, or closes it when it already is. */
export function toggleSheet(): void {
  sheetMode.update((m) => (m === 'pinned' ? 'closed' : 'pinned'))
}
