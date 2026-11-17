// coach.ts is the store-free side of the shortcut coach in the new design. It
// tells an action done by mouse from one done by its key, holds the rules for
// when a hint may show, and builds the sheet of shortcuts that apply to what
// is on screen.
//
// Actions report themselves through noteAction from the few places they all
// pass through (the app dispatcher, the compose, move and snooze stores, the
// archive and delete calls). Whether the user clicked or pressed a key is read
// off the last input event, recorded by trackInput, so no button has to say
// how it was used.

import { composeActions, type ShortcutAction } from './shortcuts'

/** How an action was reached: by pointer, or by its own shortcut. */
export type CoachOrigin = 'pointer' | 'shortcut'

/** Receives each action the coach hears about. */
export type CoachListener = (action: ShortcutAction, origin: CoachOrigin) => void

/** How long after an input an action still counts as caused by it. */
export const originWindowMs = 3000

interface LastInput {
  kind: 'key' | 'pointer'
  at: number
  seq: number
}

let last: LastInput | null = null
let seq = 0
// the input each action was last reported under, so one click that archives
// ten messages is one archive.
const noted = new Map<string, number>()
const listeners = new Set<CoachListener>()

/** Records an input event as the cause of whatever happens next. */
export function recordInput(kind: 'key' | 'pointer', now = Date.now()): void {
  seq += 1
  last = { kind, at: now, seq }
}

function isBareModifier(key: string): boolean {
  return key === 'Shift' || key === 'Control' || key === 'Alt' || key === 'Meta'
}

/**
 * Listens for key and pointer presses in the capture phase, ahead of every
 * handler that could act on them. Returns the function that stops listening.
 */
export function trackInput(target: Window = window): () => void {
  const onKey = (event: KeyboardEvent): void => {
    if (!isBareModifier(event.key)) {
      recordInput('key')
    }
  }
  const onPointer = (): void => recordInput('pointer')
  target.addEventListener('keydown', onKey, true)
  target.addEventListener('pointerdown', onPointer, true)
  return () => {
    target.removeEventListener('keydown', onKey, true)
    target.removeEventListener('pointerdown', onPointer, true)
  }
}

/** Subscribes to the actions the coach hears about. */
export function onCoachAction(listener: CoachListener): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

function emit(action: ShortcutAction, origin: CoachOrigin): void {
  for (const listener of listeners) {
    listener(action, origin)
  }
}

/** Reports that a key press matched the action's shortcut. */
export function noteShortcut(action: ShortcutAction): void {
  if (last) {
    noted.set(action, last.seq)
  }
  emit(action, 'shortcut')
}

/**
 * Reports that an action ran. It counts as done by mouse when the input just
 * before it was a pointer press; a key press, or no recent input at all, says
 * nothing about the mouse and is ignored.
 */
export function noteAction(action: ShortcutAction, now = Date.now()): void {
  if (!last || now - last.at > originWindowMs || noted.get(action) === last.seq) {
    return
  }
  noted.set(action, last.seq)
  if (last.kind === 'pointer') {
    emit(action, 'pointer')
  }
}

/** Per action: how often its hint showed, and whether its key has been used. */
export interface CoachCounters {
  shown: Record<string, number>
  learned: Record<string, boolean>
}

/** How many times one action's hint shows at most. */
export const hintLimit = 3

/** Counters with nothing shown and nothing learned. */
export function emptyCounters(): CoachCounters {
  return { shown: {}, learned: {} }
}

/** Reads stored counters, falling back to empty ones for anything malformed. */
export function parseCounters(raw: string): CoachCounters {
  try {
    const parsed = JSON.parse(raw) as Partial<CoachCounters>
    return {
      shown: parsed.shown && typeof parsed.shown === 'object' ? parsed.shown : {},
      learned: parsed.learned && typeof parsed.learned === 'object' ? parsed.learned : {},
    }
  } catch {
    return emptyCounters()
  }
}

/**
 * Whether the hint for an action may show: it has a key, the key has not been
 * used for it yet, and its hint has not run out.
 */
export function hintAllowed(counters: CoachCounters, action: string, combo: string): boolean {
  return combo !== '' && !counters.learned[action] && (counters.shown[action] ?? 0) < hintLimit
}

/** The counters after one more showing of an action's hint. */
export function withShown(counters: CoachCounters, action: string): CoachCounters {
  return { ...counters, shown: { ...counters.shown, [action]: (counters.shown[action] ?? 0) + 1 } }
}

/** The counters after the action's key was used. */
export function withLearned(counters: CoachCounters, action: string): CoachCounters {
  return { ...counters, learned: { ...counters.learned, [action]: true } }
}

/** What is on screen, as far as the sheet cares. */
export interface SheetContext {
  /** The space showing. */
  space: string
  /** Every space the app offers, for the keys that switch between them. */
  spaces: string[]
  /** The spaces in the navigation, in order, for the number keys. */
  numbered?: string[]
  /** A message is open or selected. */
  message: boolean
  /** A composer is open, and whether it holds the focus. */
  composer: boolean
  composerFocused: boolean
  /** Vim navigation of the list is on. */
  vim: boolean
  /** Reading-pane tabs are open. */
  tabs: boolean
  /** The inbox is split into tabs. */
  splits?: boolean
  /** Actions a space other than mail answers to, with their labels. */
  spaceActions: { action: string; labelKey: string }[]
}

/** One line of the sheet: what it does and the keys, ready to show. */
export interface SheetItem {
  id: string
  labelKey: string
  keys: string[]
}

/** The groups of the sheet. */
export type SheetGroupId = 'navigation' | 'message' | 'list' | 'composer' | 'space'

/** A titled group of lines. */
export interface SheetGroup {
  id: SheetGroupId
  titleKey: string
  items: SheetItem[]
}

/** What builds a sheet besides the context. */
export interface SheetSource {
  /** The live combo of each action; empty means unbound. */
  bindings: Record<string, string>
  /** The label key of an action. */
  labelFor: (action: string) => string
  /** Turns a combo into what the keycaps show. */
  format: (combo: string) => string
}

const messageActions = [
  'reply',
  'reply-all',
  'forward',
  'archive',
  'done',
  'delete-message',
  'move-to',
  'flag',
  'flag-color',
  'mark-unread',
  'mark-read',
  'snooze',
  'toggle-spam',
  'unsubscribe',
  'mark-vip',
  'toggle-pin',
  'toggle-mute',
  'edit-note',
  'create-task',
  'add-to-project',
  'open-in-tab',
  'download-offline',
  'remove-offline',
  'export-pdf',
]

const mailSpaceActions = [
  'compose',
  'open-attachments',
  'toggle-sender-panel',
  'open-newsletters',
  'open-insights',
  'new-folder',
  'empty-trash',
]

/**
 * Builds the sheet for what is on screen. Only bound actions are listed, and
 * groups left empty are dropped. The composer comes first while it has the
 * focus, since that is where the keys go.
 */
export function buildSheet(ctx: SheetContext, src: SheetSource): SheetGroup[] {
  const item = (action: string, labelKey = src.labelFor(action)): SheetItem | null => {
    const combo = src.bindings[action] ?? ''
    return combo ? { id: action, labelKey, keys: [src.format(combo)] } : null
  }
  const items = (list: (SheetItem | null)[]): SheetItem[] => list.filter((i): i is SheetItem => i !== null)
  const mail = ctx.space === 'mail'
  // a space lists its own key and its number key on one line.
  const spaceItem = (key: string): SheetItem | null => {
    const action = `go-${key}`
    const place = (ctx.numbered ?? []).indexOf(key)
    const combos = [src.bindings[action] ?? '', place >= 0 && place < 9 ? (src.bindings[`go-space-${place + 1}`] ?? '') : '']
    const keys = combos.filter((c) => c !== '').map(src.format)
    return keys.length > 0 ? { id: action, labelKey: src.labelFor(action), keys } : null
  }

  const navigation: SheetGroup = {
    id: 'navigation',
    titleKey: 'coach.group.navigation',
    items: items([
      item('command-palette'),
      item('search'),
      item('nav-back'),
      item('nav-forward'),
      ...ctx.spaces.filter((s) => s !== ctx.space).map(spaceItem),
      ...(ctx.space !== 'calendar' ? [item('calendar-quick-add')] : []),
      ...(mail ? [item('next-pane'), item('prev-pane'), item('focus-sidebar'), item('focus-list'), item('focus-reader')] : []),
      item('sync'),
      item('toggle-my-day'),
      item('focus-mode'),
      item('shortcut-sheet'),
      item('preferences'),
    ]),
  }

  const message: SheetGroup = {
    id: 'message',
    titleKey: 'coach.group.message',
    items: mail && ctx.message ? items(messageActions.map((a) => item(a))) : [],
  }

  const listItems: (SheetItem | null)[] = []
  if (mail && !ctx.composerFocused) {
    listItems.push({ id: 'list-move', labelKey: 'coach.list.move', keys: [src.format('arrowup'), src.format('arrowdown')] })
    listItems.push({ id: 'list-open', labelKey: 'coach.list.open', keys: [src.format('enter')] })
    if (ctx.vim) {
      listItems.push({ id: 'vim-move', labelKey: 'coach.list.vimMove', keys: ['j', 'k'] })
      listItems.push({ id: 'vim-ends', labelKey: 'coach.list.vimEnds', keys: ['gg', 'G'] })
    }
    if (ctx.splits) {
      listItems.push(item('next-split'), item('prev-split'))
    }
    listItems.push(item('next-view'), item('prev-view'), item('new-view'))
    if (ctx.tabs) {
      listItems.push(item('close-tab'))
    }
  }
  const list: SheetGroup = { id: 'list', titleKey: 'coach.group.list', items: items(listItems) }

  const composer: SheetGroup = {
    id: 'composer',
    titleKey: 'coach.group.composer',
    items: ctx.composer
      ? items([item('send'), ...composeActions.map((a) => item(a)), item('close-window', 'coach.composer.close')])
      : [],
  }

  const space: SheetGroup = {
    id: 'space',
    titleKey: `space.${ctx.space}`,
    items: mail
      ? items(mailSpaceActions.map((a) => item(a)))
      : items(ctx.spaceActions.map((a) => item(a.action, a.labelKey))),
  }

  const order = ctx.composerFocused
    ? [composer, navigation, message, list, space]
    : [navigation, message, list, composer, space]
  return order.filter((g) => g.items.length > 0)
}

/**
 * Narrows a sheet to the lines whose label or keys contain the query. label
 * resolves a label key to the text shown.
 */
export function filterSheet(groups: SheetGroup[], query: string, label: (key: string) => string): SheetGroup[] {
  const q = query.trim().toLowerCase()
  if (!q) {
    return groups
  }
  return groups
    .map((g) => ({
      ...g,
      items: g.items.filter((i) => label(i.labelKey).toLowerCase().includes(q) || i.keys.some((k) => k.toLowerCase().includes(q))),
    }))
    .filter((g) => g.items.length > 0)
}
