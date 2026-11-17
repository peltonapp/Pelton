// spaces.ts lists the top-level areas of the app once, so the rail, the sidebar
// footer switcher and the top bar tabs all show the same entries in the same
// order.

import mailIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
import contactsIcon from '@phosphor-icons/core/assets/duotone/address-book-duotone.svg?raw'
import calendarIcon from '@phosphor-icons/core/assets/duotone/calendar-blank-duotone.svg?raw'
import tasksIcon from '@phosphor-icons/core/assets/duotone/list-checks-duotone.svg?raw'
import projectsIcon from '@phosphor-icons/core/assets/duotone/kanban-duotone.svg?raw'
import notesIcon from '@phosphor-icons/core/assets/duotone/notebook-duotone.svg?raw'
import feedsIcon from '@phosphor-icons/core/assets/duotone/rss-duotone.svg?raw'
import chatIcon from '@phosphor-icons/core/assets/duotone/chats-circle-duotone.svg?raw'
import { derived, get } from 'svelte/store'
import { IconAdjustmentsHorizontal, IconArrowDown, IconArrowLeft, IconArrowRight, IconArrowUp, IconEye, IconEyeOff } from '@tabler/icons-svelte'
import { prefs, setHiddenSpaces, setSpaceOrder } from '../stores/prefs'
import { openSettingsAt } from '../stores/settingsnav'
import type { MenuEntry } from '../stores/contextmenu'
import { applyShownOrder, canHide, hiddenSpaces, moveInList, moveShown, orderSpaces, serializeHidden, serializeOrder } from './spaceorder'
import type { Space } from './types'
import type { MailDropOptions } from '../stores/maildrag'
import { contactSeed } from '../stores/contacts'
import { calendarSeed } from '../stores/calendar'
import { taskSeed } from '../stores/tasks'
import { openSpace, space } from '../stores/space'
import { mailDropHint } from '../components/calendar/maildrop'
import { createTasksFromMail } from './taskmail'
import { pickProjectFor } from './projectpicker'
import { dropMailOnNotes } from './notemail'
import { activeNote } from '../stores/notes'
import { t } from './i18n'

/** One entry of the space switcher. */
export interface SpaceEntry {
  key: Space
  /** Localization key of the name. */
  labelKey: string
  /** Raw Phosphor svg. */
  icon: string
}

/**
 * What a space does with mail dropped on its entry in the switcher: contacts
 * starts a new contact from the sender, the calendar a new event from the
 * message, tasks a task from each message, projects asks which project the
 * conversations go to, notes links in the open note or a note about the first
 * message, mail takes nothing.
 */
export function spaceDrop(key: Space): MailDropOptions {
  return {
    accepts: () => key === 'contacts' || key === 'calendar' || key === 'tasks' || key === 'projects' || key === 'notes',
    onDrop: (items, info) => {
      const first = items[0]
      if (key === 'notes' && first) {
        void dropMailOnNotes(items.map((m) => m.id))
      }
      if (key === 'contacts' && first) {
        contactSeed.set({ name: first.fromName, email: first.fromAddress })
        openSpace('contacts')
      }
      if (key === 'calendar' && first) {
        calendarSeed.set({ messageIds: items.map((m) => m.id), at: null, mode: 'time', shift: info.shift })
        openSpace('calendar')
      }
      if (key === 'tasks' && first) {
        const ids = items.map((m) => m.id)
        // the Tasks screen takes the drop when it is showing; elsewhere the
        // tasks are made in place, so the mail stays on screen.
        if (get(space) === 'tasks') {
          taskSeed.set(ids)
        } else {
          void createTasksFromMail(ids)
        }
      }
      if (key === 'projects' && first) {
        pickProjectFor(items.map((m) => m.id))
      }
    },
    hint: (items, shift) => {
      if (key === 'calendar') {
        return mailDropHint(items.length, shift, false)
      }
      if (key === 'tasks') {
        const tr = get(t)
        return items.length > 1 ? tr('tasks.drop.many').replace('{n}', String(items.length)) : tr('tasks.drop.one')
      }
      if (key === 'projects') {
        return get(t)('projects.drop.pick')
      }
      if (key === 'notes') {
        const tr = get(t)
        return get(space) === 'notes' && get(activeNote) ? tr('notes.drop.insert') : tr('notes.drop.new')
      }
      return ''
    },
  }
}

/** The areas the app offers, in display order. */
export const spaces: SpaceEntry[] = [
  { key: 'mail', labelKey: 'space.mail', icon: mailIcon },
  { key: 'calendar', labelKey: 'space.calendar', icon: calendarIcon },
  { key: 'tasks', labelKey: 'space.tasks', icon: tasksIcon },
  { key: 'projects', labelKey: 'space.projects', icon: projectsIcon },
  { key: 'contacts', labelKey: 'space.contacts', icon: contactsIcon },
  { key: 'notes', labelKey: 'space.notes', icon: notesIcon },
  { key: 'feeds', labelKey: 'space.feeds', icon: feedsIcon },
  { key: 'chat', labelKey: 'space.chat', icon: chatIcon },
]

/** Whether a space is switched on: chat only shows once the user turned it on. */
export function spaceEnabled(key: Space, p: { chatEnabled: boolean }): boolean {
  return key !== 'chat' || p.chatEnabled
}

const allKeys = spaces.map((s) => s.key)
const byKey = new Map(spaces.map((s) => [s.key, s]))

/** A space as the navigation lists it. */
export interface NavEntry extends SpaceEntry {
  /** Left out of the navigation by the user. */
  hidden: boolean
  /** Shown only because it is open while hidden. */
  transient?: boolean
}

/** Every switched-on space in the user's order, hidden ones included and marked. */
export const orderedSpaces = derived(prefs, ($p): NavEntry[] => {
  const hidden = hiddenSpaces(allKeys, $p.hiddenSpaces)
  return orderSpaces(allKeys, $p.spaceOrder)
    .filter((key) => spaceEnabled(key, $p))
    .map((key) => ({ ...(byKey.get(key) as SpaceEntry), hidden: hidden.has(key) }))
})

/** The spaces in the navigation: the user's order, without the hidden ones. */
export const visibleSpaces = derived(orderedSpaces, ($o) => $o.filter((s) => !s.hidden))

/**
 * What the rail and the switchers draw: the visible spaces, plus a hidden one
 * at its place in the order while it is open, so the open space always has an
 * entry.
 */
export const navSpaces = derived([orderedSpaces, space], ([$o, $space]): NavEntry[] =>
  $o.filter((s) => !s.hidden || s.key === $space).map((s) => (s.hidden ? { ...s, transient: true } : s)),
)

function currentOrder(): { order: Space[]; hidden: Set<Space> } {
  const p = get(prefs)
  return { order: orderSpaces(allKeys, p.spaceOrder), hidden: hiddenSpaces(allKeys, p.hiddenSpaces) }
}

function storeOrder(order: Space[]): void {
  setSpaceOrder(serializeOrder(allKeys, order))
}

/** Leaves a space out of the navigation, or puts it back. Mail always stays. */
export function setSpaceHidden(key: Space, hide: boolean): void {
  if (hide && !canHide(key)) {
    return
  }
  const { order, hidden } = currentOrder()
  if (hide) {
    hidden.add(key)
  } else {
    hidden.delete(key)
  }
  setHiddenSpaces(serializeHidden(order, hidden))
}

/** Moves a space past its neighbour among the spaces the navigation shows. */
export function moveSpaceShown(key: Space, delta: number): void {
  const { order, hidden } = currentOrder()
  storeOrder(moveShown(order, hidden, key, delta))
}

// the spaces switched off keep their slots while the listed ones move, so a
// move never passes one that is not on screen.
function switchedOff(): Set<Space> {
  const p = get(prefs)
  return new Set(allKeys.filter((k) => !spaceEnabled(k, p)))
}

/** Moves a space past its neighbour in the settings list, hidden ones included. */
export function moveSpaceListed(key: Space, delta: number): void {
  const { order } = currentOrder()
  const off = switchedOff()
  storeOrder(applyShownOrder(order, off, moveInList(order.filter((k) => !off.has(k)), key, delta)))
}

/** Puts the spaces of the settings list in a new order, after a drag. */
export function reorderListedSpaces(keys: string[]): void {
  const known = keys.filter((k): k is Space => byKey.has(k as Space))
  storeOrder(applyShownOrder(currentOrder().order, switchedOff(), known))
}

/** Puts the spaces the navigation shows in a new order, after a drag. */
export function reorderShownSpaces(keys: string[]): void {
  const { order, hidden } = currentOrder()
  const known = keys.filter((k): k is Space => byKey.has(k as Space))
  storeOrder(applyShownOrder(order, hidden, known))
}

/** Puts every space back in the default order, all of them shown. */
export function resetSpaceNav(): void {
  setSpaceOrder('')
  setHiddenSpaces('')
}

/** The settings row where the navigation is customized. */
export const navSettingsAnchor = 'layout#spaces'

/**
 * The right-click menu of a space in the navigation: hide or show it, move it
 * along, and the way to the settings row. axis says which way the entries run,
 * for the wording of the moves.
 */
export function spaceMenu(entry: NavEntry, axis: 'x' | 'y'): MenuEntry[] {
  const tr = get(t)
  const shown = get(visibleSpaces)
  const at = shown.findIndex((s) => s.key === entry.key)
  const out: MenuEntry[] = []
  if (entry.transient) {
    out.push({ label: tr('spaces.menu.show'), icon: IconEye, action: () => setSpaceHidden(entry.key, false) })
  } else {
    if (at > 0) {
      out.push({ label: tr(axis === 'x' ? 'spaces.menu.moveLeft' : 'spaces.menu.moveUp'), icon: axis === 'x' ? IconArrowLeft : IconArrowUp, action: () => moveSpaceShown(entry.key, -1) })
    }
    if (at >= 0 && at < shown.length - 1) {
      out.push({ label: tr(axis === 'x' ? 'spaces.menu.moveRight' : 'spaces.menu.moveDown'), icon: axis === 'x' ? IconArrowRight : IconArrowDown, action: () => moveSpaceShown(entry.key, 1) })
    }
    if (canHide(entry.key)) {
      out.push({ label: tr('spaces.menu.hide'), icon: IconEyeOff, action: () => setSpaceHidden(entry.key, true) })
    }
  }
  if (out.length > 0) {
    out.push('separator')
  }
  out.push({ label: tr('spaces.menu.customize'), icon: IconAdjustmentsHorizontal, action: () => openSettingsAt(navSettingsAnchor) })
  return out
}
