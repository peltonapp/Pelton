// rowactions.ts is the catalogue of the actions a message row offers outside
// its menu: the quick buttons over a hovered row and the trackpad swipes. Each
// action names the shortcut it mirrors, so the button's tooltip shows the
// user's own binding and the action runs the same way the key does. This
// module only describes and configures; lib/rowactionrun.ts runs them.

import archiveIcon from '@phosphor-icons/core/assets/duotone/archive-duotone.svg?raw'
import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
import doneIcon from '@phosphor-icons/core/assets/duotone/check-circle-duotone.svg?raw'
import snoozeIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
import pinIcon from '@phosphor-icons/core/assets/duotone/push-pin-duotone.svg?raw'
import unpinIcon from '@phosphor-icons/core/assets/duotone/push-pin-slash-duotone.svg?raw'
import readIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-open-duotone.svg?raw'
import unreadIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
import flagIcon from '@phosphor-icons/core/assets/duotone/flag-duotone.svg?raw'
import muteIcon from '@phosphor-icons/core/assets/duotone/bell-slash-duotone.svg?raw'
import unmuteIcon from '@phosphor-icons/core/assets/duotone/bell-duotone.svg?raw'
import moveIcon from '@phosphor-icons/core/assets/duotone/folder-open-duotone.svg?raw'
import spamIcon from '@phosphor-icons/core/assets/duotone/warning-octagon-duotone.svg?raw'
import projectIcon from '@phosphor-icons/core/assets/duotone/kanban-duotone.svg?raw'
import taskIcon from '@phosphor-icons/core/assets/duotone/check-square-duotone.svg?raw'
import { scriptAction, type ShortcutAction } from './shortcuts'
import { scriptIcon } from './scripticons'
import { scriptText } from './scriptmark'
import type { PlacedScript } from './scripting'

/** A script placed on the row buttons or the swipes: "script:" and its id. */
export type ScriptRowActionId = `script:${number}`

/** An action a row offers as a quick button or a swipe. */
export type RowActionId = BuiltinRowActionId | ScriptRowActionId

/** The row actions the app itself provides. */
export type BuiltinRowActionId =
  | 'archive'
  | 'delete'
  | 'done'
  | 'snooze'
  | 'pin'
  | 'read'
  | 'flag'
  | 'mute'
  | 'move'
  | 'spam'
  | 'project'
  | 'task'

/** Every row action, in the order the settings list them. */
export const rowActionIds: readonly BuiltinRowActionId[] = [
  'archive',
  'delete',
  'done',
  'snooze',
  'pin',
  'read',
  'flag',
  'mute',
  'move',
  'spam',
  'project',
  'task',
]

/** The most quick buttons a row shows. */
export const maxRowActions = 5

/** The quick buttons a row shows until the user picks others. */
export const defaultRowActions: readonly RowActionId[] = ['archive', 'delete', 'done', 'snooze', 'pin']

/** What a row's state changes about an action: its name, its icon and its key. */
export interface RowActionState {
  /** Unread rather than read, for the read toggle. */
  seen: boolean
  flagged: boolean
  pinned: boolean
  muted: boolean
  /** The message sits in its account's spam folder. */
  inSpam: boolean
}

/** One action as a button or a swipe shows it. */
export interface RowActionView {
  id: RowActionId
  /** Localization key of what pressing it does now. */
  labelKey: string
  /** A name shown instead of the label key, for a script. */
  label?: string
  /** Set for a script, to its name: the action carries the script mark. */
  script?: string
  /** Raw Phosphor svg. */
  icon: string
  /** The shortcut it mirrors, for the tooltip. */
  shortcut: ShortcutAction
  /** Destructive: drawn in the danger color on hover and as a swipe. */
  danger: boolean
}

/**
 * Describes an action for a row in a given state: a toggle names the way it
 * would go, so a read row offers "Mark unread". A script is described from
 * the scripts placed where it is used; one that is not among them has no
 * name and the default icon.
 */
export function rowActionView(id: RowActionId, s: RowActionState, scripts: readonly PlacedScript[] = []): RowActionView {
  const view = (labelKey: string, icon: string, shortcut: ShortcutAction, danger = false): RowActionView => ({
    id,
    labelKey,
    icon,
    shortcut,
    danger,
  })
  const scriptId = scriptRowActionId(id)
  if (scriptId !== null) {
    const script = scripts.find((p) => p.scriptId === scriptId)
    const name = scriptText(script?.name ?? '')
    return { ...view('', scriptIcon(script?.options.icon), scriptAction(scriptId)), label: name, script: name }
  }
  switch (id as BuiltinRowActionId) {
    case 'archive':
      return view('action.archive', archiveIcon, 'archive')
    case 'delete':
      return view('action.delete', trashIcon, 'delete-message', true)
    case 'done':
      return view('shortcut.done', doneIcon, 'done')
    case 'snooze':
      return view('shortcut.snooze', snoozeIcon, 'snooze')
    case 'pin':
      return s.pinned ? view('pin.unpin', unpinIcon, 'toggle-pin') : view('pin.pin', pinIcon, 'toggle-pin')
    case 'read':
      return s.seen
        ? view('shortcut.markUnread', unreadIcon, 'mark-unread')
        : view('shortcut.markRead', readIcon, 'mark-read')
    case 'flag':
      return s.flagged ? view('messageList.unflag', flagIcon, 'flag') : view('messageList.flag', flagIcon, 'flag')
    case 'mute':
      return s.muted ? view('mute.unmute', unmuteIcon, 'toggle-mute') : view('mute.mute', muteIcon, 'toggle-mute')
    case 'move':
      return view('messageList.menu.moveTo', moveIcon, 'move-to')
    case 'spam':
      return s.inSpam ? view('spam.notSpam', spamIcon, 'toggle-spam') : view('spam.mark', spamIcon, 'toggle-spam')
    case 'project':
      return view('projects.addTo', projectIcon, 'add-to-project')
    case 'task':
      return view('shortcut.createTask', taskIcon, 'create-task')
  }
}

/** The name an action carries in settings, where no row state applies. */
export function rowActionSettingKey(id: BuiltinRowActionId): string {
  return `rowActions.action.${id}`
}

/** The row action id of a script. */
export function scriptRowAction(scriptId: number): ScriptRowActionId {
  return `script:${scriptId}`
}

/** The script a row action runs, or null for an action of the app's own. */
export function scriptRowActionId(id: string): number | null {
  const m = /^script:(\d+)$/.exec(id)
  return m ? Number(m[1]) : null
}

/** Whether a string names a row action. */
export function isRowAction(value: string): value is RowActionId {
  return (rowActionIds as readonly string[]).includes(value) || scriptRowActionId(value) !== null
}

/** A swipe action, or 'none' when it is a script that is not among the placed ones. */
export function placedSwipeAction(action: string, scripts: readonly PlacedScript[]): string {
  const scriptId = scriptRowActionId(action)
  return scriptId !== null && !scripts.some((p) => p.scriptId === scriptId) ? 'none' : action
}

/** The actions with the scripts that are not among the placed ones left out. */
export function placedRowActions(ids: readonly RowActionId[], scripts: readonly PlacedScript[]): RowActionId[] {
  return ids.filter((id) => {
    const scriptId = scriptRowActionId(id)
    return scriptId === null || scripts.some((p) => p.scriptId === scriptId)
  })
}

/**
 * Reads the stored quick button list: a comma separated list of action ids.
 * Unknown ids and repeats are dropped and the list is cut to the maximum, so a
 * hand-edited or older value still yields a usable row.
 */
export function parseRowActions(value: string): RowActionId[] {
  const out: RowActionId[] = []
  for (const part of value.split(',')) {
    const id = part.trim()
    if (isRowAction(id) && !out.includes(id)) {
      out.push(id)
    }
    if (out.length === maxRowActions) {
      break
    }
  }
  return out
}

/** Writes a quick button list back to its stored form. */
export function serializeRowActions(ids: readonly RowActionId[]): string {
  return parseRowActions(ids.join(',')).join(',')
}

/**
 * Turns one action on or off in a list. Turning one on adds it at the end and
 * is refused once the list is full; the list is returned unchanged then.
 */
export function toggleRowAction(ids: readonly RowActionId[], id: RowActionId, on: boolean): RowActionId[] {
  if (!on) {
    return ids.filter((x) => x !== id)
  }
  if (ids.includes(id) || ids.length >= maxRowActions) {
    return [...ids]
  }
  return [...ids, id]
}

/** Moves the action at index by one place up (-1) or down (1), within bounds. */
export function moveRowAction(ids: readonly RowActionId[], index: number, by: -1 | 1): RowActionId[] {
  const to = index + by
  if (index < 0 || index >= ids.length || to < 0 || to >= ids.length) {
    return [...ids]
  }
  const next = [...ids]
  const [id] = next.splice(index, 1)
  next.splice(to, 0, id)
  return next
}

/**
 * Whether an action means something on this message: mute needs a
 * conversation and the spam marks need a spam folder to move to or from.
 */
export function rowActionApplies(id: RowActionId, ctx: { threadId: number; spam: 'spam' | 'markable' | 'none' }): boolean {
  if (id === 'mute') {
    return ctx.threadId !== 0
  }
  if (id === 'spam') {
    return ctx.spam !== 'none'
  }
  return true
}
