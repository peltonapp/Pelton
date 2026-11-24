// shortcuts.ts (store) holds the live, user-customizable key bindings: a map from
// each app action to its current combo, seeded from the defaults and overlaid
// with the user's overrides (persisted in the backend settings table as json).
// App.svelte matches keydown events against this map, and the settings panel
// edits it.

import { writable, derived, get } from 'svelte/store'
import { getSetting, setSetting } from '../lib/api'
import { shortcuts as defaults, scriptAction, scriptOfAction, type Shortcut, type ShortcutAction } from '../lib/shortcuts'
import { shortcutLabel } from '../lib/i18n'
import { prefs } from './prefs'
import { placedScripts } from './scripts'

const KEY = 'keyboard_shortcuts'

type Bindings = Record<ShortcutAction, string>

// defaultBindings builds the action -> default-combo map from the registry.
function defaultBindings(): Bindings {
  const map = {} as Bindings
  for (const s of defaults) {
    map[s.action] = s.combo
  }
  return map
}

export const bindings = writable<Bindings>(defaultBindings())

// recording is true while the settings panel is capturing a new combo, so the
// global keydown handler stands down and does not fire actions mid-capture.
export const recording = writable<boolean>(false)

// initShortcuts loads persisted overrides once at startup, overlaying them on the
// defaults so a newly added action always has a binding.
export async function initShortcuts(): Promise<void> {
  try {
    const { value, found } = await getSetting(KEY)
    if (found && value) {
      const parsed = JSON.parse(value) as Partial<Bindings>
      bindings.set({ ...defaultBindings(), ...parsed } as Bindings)
    }
  } catch {
    // ignore: keep the defaults.
  }
}

// persist writes only the entries that differ from the defaults, so resetting an
// action removes its override entirely.
function persist(map: Bindings): void {
  const base = defaultBindings()
  const overrides: Partial<Bindings> = {}
  for (const action of Object.keys(map) as ShortcutAction[]) {
    if (map[action] !== (base[action] ?? '')) {
      overrides[action] = map[action]
    }
  }
  void setSetting(KEY, JSON.stringify(overrides))
}

// setBinding rebinds one action to a new combo and persists the change.
export function setBinding(action: ShortcutAction, combo: string): void {
  bindings.update((map) => {
    const next = { ...map, [action]: combo }
    persist(next)
    return next
  })
}

// resetBinding restores one action to its default combo.
export function resetBinding(action: ShortcutAction): void {
  const base = defaultBindings()
  setBinding(action, base[action] ?? '')
}

// resetAll restores every shortcut to its default.
export function resetAll(): void {
  const base = defaultBindings()
  bindings.set(base)
  persist(base)
}

/**
 * The scripts placed as shortcuts as actions without a default key, named
 * for the script. An action of a script no longer placed is not among them.
 */
export const scriptShortcuts = derived(placedScripts, ($placed): Shortcut[] =>
  ($placed['shortcut'] ?? []).map((s) => ({ action: scriptAction(s.scriptId), combo: '', labelKey: '', label: s.name })),
)

/** The bindings with the keys of scripts that are not placed as shortcuts left out. */
export const activeBindings = derived([bindings, scriptShortcuts], ([$bindings, $scripts]) => {
  const live = new Set<string>($scripts.map((s) => s.action))
  const out: Record<string, string> = {}
  for (const [action, combo] of Object.entries($bindings)) {
    if (scriptOfAction(action) === null || live.has(action)) {
      out[action] = combo
    }
  }
  return out as Bindings
})

// conflictsFor returns the action currently bound to combo other than the given
// one, or null. used by the editor to warn before assigning a duplicate.
export function conflictsFor(action: ShortcutAction, combo: string): ShortcutAction | null {
  const map = get(activeBindings)
  for (const other of Object.keys(map) as ShortcutAction[]) {
    if (other !== action && map[other] === combo) {
      return other
    }
  }
  return null
}

// menuHint renders an action's current shortcut for a context-menu row, or an
// empty string when the action is unbound or hints are turned off. Menus pass
// it straight into MenuItem.hint, so an unbound action simply has no hint
// rather than an empty box (#329).
export function menuHint(action: ShortcutAction): string {
  if (!get(prefs).showShortcutHints) {
    return ''
  }
  const combo = get(bindings)[action]
  return combo ? shortcutLabel(combo) : ''
}

// shortcutTitle appends an action's key to a button tooltip, so hovering a
// toolbar button says which key does the same thing. It is a derived store
// rather than a plain function because a tooltip is rendered once and stays:
// toggling the setting or rebinding the action has to update it in place,
// unlike a menu, which is rebuilt every time it opens.
//
// Several actions can be listed for one button, first bound one wins. That is
// for a button whose own action is unbound by default but which a broader
// shortcut already reaches: closing a tab answers to close-window until the
// user binds close-tab to something.
export const shortcutTitle = derived(
  [prefs, bindings],
  ([$prefs, $bindings]) =>
    (label: string, ...actions: ShortcutAction[]): string => {
      if (!$prefs.showShortcutHints) {
        return label
      }
      const combo = actions.map((action) => $bindings[action]).find(Boolean)
      return combo ? `${label}  ${shortcutLabel(combo)}` : label
    },
)
