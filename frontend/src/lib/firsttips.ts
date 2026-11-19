// firsttips.ts holds the one-time tips of the new design and the rules for
// when one may show: tips switched on, nothing full-window open, no other tip
// on screen, not seen before, and no more than one new tip per session. a tip
// that names keys only shows while those keys are bound, and is retired
// unseen once the shortcut coach knows every one of them has been used. it is
// pure, so the rules can be tested without stores.

/** The tips there are. */
export type TipId =
  | 'reader-view'
  | 'archive-undo'
  | 'space-keys'
  | 'mail-drag'
  | 'dark-mail'
  | 'search-operators'
  | 'compose-send'
  | 'calendar-quick-add'
  | 'notes-links'

/** One tip. */
export interface TipDef {
  id: TipId
  /** Locale key of the text; {name} placeholders become the keys in `keys`. */
  textKey: string
  /** Placeholder name to the shortcut action whose live combo fills it. */
  keys: Record<string, string>
  /** data-tip-anchor value of the control the tip hangs from; none shows a bar. */
  anchor?: string
}

/** Every tip, at most about ten. */
export const tipDefs: readonly TipDef[] = [
  { id: 'reader-view', textKey: 'tips.readerView', keys: { key: 'reading-view' } },
  { id: 'archive-undo', textKey: 'tips.archiveUndo', keys: { undo: 'undo' }, anchor: 'history' },
  { id: 'space-keys', textKey: 'tips.spaceKeys', keys: { first: 'go-space-1', back: 'nav-back', forward: 'nav-forward' }, anchor: 'nav' },
  { id: 'mail-drag', textKey: 'tips.mailDrag', keys: {} },
  { id: 'dark-mail', textKey: 'tips.darkMail', keys: {}, anchor: 'mail-colors' },
  { id: 'search-operators', textKey: 'tips.searchOperators', keys: {}, anchor: 'search' },
  { id: 'compose-send', textKey: 'tips.composeSend', keys: { archive: 'compose-send-archive', remind: 'compose-send-remind' } },
  { id: 'calendar-quick-add', textKey: 'tips.calendarQuickAdd', keys: { key: 'calendar-quick-add' } },
  { id: 'notes-links', textKey: 'tips.notesLinks', keys: {} },
]

/** The tip with this id. */
export function tipDef(id: TipId): TipDef {
  return tipDefs.find((d) => d.id === id) as TipDef
}

/** How many new tips one session shows at most. */
export const tipsPerSession = 1

/** Everything besides the tip itself that decides whether it shows. */
export interface TipGate {
  /** The showTips pref, and the new design. */
  enabled: boolean
  /** Onboarding, settings or another full-window screen is open. */
  suspended: boolean
  /** A tip or a coach hint is on screen. */
  busy: boolean
  seen: ReadonlySet<string>
  shownThisSession: number
  /** The live combo of each action; empty or missing means unbound. */
  bindings: Record<string, string>
  /** Whether the shortcut coach has seen the action's key used. */
  learned: (action: string) => boolean
}

/**
 * What to do with a tip whose context just came up. show puts it on screen;
 * retire marks it seen without showing it, since the user already uses its
 * keys; every other answer leaves it for a later occasion or for good.
 */
export type TipVerdict = 'show' | 'retire' | 'off' | 'suspended' | 'busy' | 'seen' | 'session' | 'unbound'

/** Applies the rules to one tip. */
export function decideTip(def: TipDef, gate: TipGate): TipVerdict {
  if (!gate.enabled) {
    return 'off'
  }
  if (gate.seen.has(def.id)) {
    return 'seen'
  }
  const actions = Object.values(def.keys)
  if (actions.some((a) => !gate.bindings[a])) {
    return 'unbound'
  }
  if (actions.length > 0 && actions.every((a) => gate.learned(a))) {
    return 'retire'
  }
  if (gate.suspended) {
    return 'suspended'
  }
  if (gate.busy) {
    return 'busy'
  }
  if (gate.shownThisSession >= tipsPerSession) {
    return 'session'
  }
  return 'show'
}

/** Reads the stored list of seen tips; anything malformed reads as none. */
export function parseSeen(raw: string): Set<string> {
  try {
    const list = JSON.parse(raw) as unknown
    return new Set(Array.isArray(list) ? list.filter((x): x is string => typeof x === 'string') : [])
  } catch {
    return new Set()
  }
}

/** The seen list as stored, sorted so it reads stable. */
export function serializeSeen(seen: ReadonlySet<string>): string {
  return JSON.stringify([...seen].sort())
}

/** A piece of a tip's text: plain words, or a key to draw as a keycap. */
export type TipPart = { text: string } | { combo: string }

/** Splits a tip's text around its {placeholders}, filling each with its live combo. */
export function tipParts(text: string, def: TipDef, bindings: Record<string, string>): TipPart[] {
  const out: TipPart[] = []
  const re = /\{(\w+)\}/g
  let at = 0
  for (let m = re.exec(text); m; m = re.exec(text)) {
    const action = def.keys[m[1]]
    if (!action) {
      continue
    }
    if (m.index > at) {
      out.push({ text: text.slice(at, m.index) })
    }
    out.push({ combo: bindings[action] ?? '' })
    at = m.index + m[0].length
  }
  if (at < text.length) {
    out.push({ text: text.slice(at) })
  }
  return out
}
