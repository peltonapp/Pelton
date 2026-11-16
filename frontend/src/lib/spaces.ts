// spaces.ts lists the top-level areas of the app once, so the rail, the sidebar
// footer switcher and the top bar tabs all show the same entries in the same
// order.

import mailIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
import contactsIcon from '@phosphor-icons/core/assets/duotone/address-book-duotone.svg?raw'
import calendarIcon from '@phosphor-icons/core/assets/duotone/calendar-blank-duotone.svg?raw'
import tasksIcon from '@phosphor-icons/core/assets/duotone/list-checks-duotone.svg?raw'
import projectsIcon from '@phosphor-icons/core/assets/duotone/kanban-duotone.svg?raw'
import notesIcon from '@phosphor-icons/core/assets/duotone/notebook-duotone.svg?raw'
import { get } from 'svelte/store'
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
]
