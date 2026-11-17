// firsttips.ts (store) runs the one-time tips of the new design. A context
// reports itself through noteTipContext; the rules in lib/firsttips decide
// whether its tip shows. A tip counts as seen the moment it shows, so it never
// comes back, and while it is up the shortcut coach keeps its own hints back
// and gives up on the keys the tip names. What has been seen is kept in the
// local settings table and nowhere else.

import { get, writable } from 'svelte/store'
import { getSetting, setSetting, SettingKeys } from '../lib/api'
import { decideTip, parseSeen, serializeSeen, tipDef, type TipDef, type TipId } from '../lib/firsttips'
import { coachHint, coachLearned, coachQuiet, retireCoachHints } from './coach'
import { bindings } from './shortcuts'
import { prefs } from './prefs'

/** The tip on screen, or null. */
export const activeTip = writable<TipDef | null>(null)

let seen = new Set<string>()
let shownThisSession = 0
let loaded = false
let suspended = false

function save(): void {
  void setSetting(SettingKeys.tipsSeen, serializeSeen(seen)).catch(() => {})
}

/** Loads which tips have been seen. Until it has, no tip shows. */
export async function initFirstTips(): Promise<void> {
  try {
    const { value, found } = await getSetting(SettingKeys.tipsSeen)
    seen = found ? parseSeen(value) : new Set()
  } catch {
    seen = new Set()
  }
  loaded = true
}

/** Holds tips back while onboarding, settings or another full-window screen is open. */
export function setTipsSuspended(value: boolean): void {
  suspended = value
  if (value) {
    dismissTip()
  }
}

/**
 * Reports that a tip's context came up. Returns whether its tip is now on
 * screen.
 */
export function noteTipContext(id: TipId): boolean {
  const def = tipDef(id)
  const p = get(prefs)
  const verdict = decideTip(def, {
    enabled: loaded && p.showTips && p.design !== 'classic',
    suspended,
    busy: get(activeTip) !== null || get(coachHint) !== null,
    seen,
    shownThisSession,
    bindings: get(bindings) as Record<string, string>,
    learned: coachLearned,
  })
  if (verdict === 'retire' || verdict === 'show') {
    seen = new Set(seen).add(id)
    save()
  }
  if (verdict !== 'show') {
    return false
  }
  shownThisSession += 1
  retireCoachHints(Object.values(def.keys))
  coachQuiet.set(true)
  activeTip.set(def)
  return true
}

/** Takes the tip off screen. It stays seen. */
export function dismissTip(): void {
  if (get(activeTip) !== null) {
    activeTip.set(null)
  }
  coachQuiet.set(false)
}

/** Forgets every tip seen, so each can show again, starting with this session. */
export function resetTips(): void {
  seen = new Set()
  shownThisSession = 0
  dismissTip()
  save()
}
