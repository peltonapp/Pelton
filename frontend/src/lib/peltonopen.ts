// peltonopen.ts follows a pelton:// link out of a note: it finds the mail,
// event, contact, task or project the link names and shows it in its space,
// or says that it is no longer there.

import { get } from 'svelte/store'
import { findEventByUID } from './api'
import { parsePeltonUrl, midFromMessageId } from './peltonlinks'
import { openMailLink } from './maillink'
import { parseEventTime } from './caldate'
import { openTask } from './taskmail'
import { t } from './i18n'
import { calendarFocus } from '../stores/calendar'
import { contacts, loadContacts } from '../stores/contacts'
import { selectedContactId } from '../stores/contactsview'
import { loadTasks, tasks } from '../stores/tasks'
import { loadProjects, openCard, openProject, projects } from '../stores/projects'
import { openSpace } from '../stores/space'
import { errorMessage, toastError, toastInfo } from '../stores/toast'

/** Shows the object a pelton:// link names. False when the link is not one. */
export async function openPeltonUrl(url: string): Promise<boolean> {
  const link = parsePeltonUrl(url)
  if (!link) {
    return false
  }
  const tr = get(t)
  try {
    switch (link.kind) {
      case 'mail':
      case 'conversation':
        await openMailLink(midFromMessageId(link.id))
        break
      case 'event': {
        const ref = await findEventByUID(link.id)
        if (!ref.found) {
          toastInfo(tr('notes.object.gone'))
          break
        }
        calendarFocus.set(parseEventTime(ref.start, ref.allDay))
        openSpace('calendar')
        break
      }
      case 'contact': {
        if (get(contacts).length === 0) {
          await loadContacts()
        }
        const id = link.id.toLowerCase()
        const found = get(contacts).find((c) => c.uid === link.id) ?? get(contacts).find((c) => c.emails.some((e) => e.value.toLowerCase() === id))
        if (!found) {
          toastInfo(tr('notes.object.gone'))
          break
        }
        selectedContactId.set(found.id)
        openSpace('contacts')
        break
      }
      case 'task': {
        if (get(tasks).length === 0) {
          await loadTasks()
        }
        const found = get(tasks).find((x) => x.uid === link.id)
        if (!found) {
          toastInfo(tr('notes.object.gone'))
          break
        }
        openTask(found.id)
        break
      }
      case 'project': {
        if (get(projects).length === 0) {
          await loadProjects()
        }
        const found = get(projects).find((p) => p.uid === link.id)
        if (!found) {
          toastInfo(tr('notes.object.gone'))
          break
        }
        if (link.sub) {
          openCard(found.id, link.sub)
        } else {
          openProject(found.id)
        }
        break
      }
    }
  } catch (err) {
    toastError(errorMessage(err))
  }
  return true
}
