// taskmail.ts turns mail into tasks: one task per message, titled with its
// subject, linked back to it by a mid: link, and due on the first date the
// mail's text names, read the way the calendar reads dates in mail. it is what
// the Create task action, its shortcut, the command palette and a drop on the
// Tasks entry all run.

import { get } from 'svelte/store'
import { createTask, deleteTask, mailEventSource } from './api'
import { datesInText } from './eventphrase'
import { loadChrono } from './chronoload'
import { dayKey } from './caldate'
import { locale, t } from './i18n'
import { openSpace } from '../stores/space'
import { defaultListId, emptyDraft, loadTasks, localZone, taskFocus, taskLists, taskSettings } from '../stores/tasks'
import { errorMessage, pushActions, toastError } from '../stores/toast'
import type { MailEventSource, TaskDraft } from './types'

/** The draft a mail turns into: its subject, its link, and the first date its text names. */
export async function draftFromMail(mail: MailEventSource, listId: number): Promise<TaskDraft> {
  const draft = { ...emptyDraft(listId), title: mail.subject, timeZone: localZone() }
  if (mail.link) {
    draft.mailLinks = [mail.link]
  }
  try {
    const chrono = await loadChrono(get(locale))
    const now = new Date()
    const found = datesInText(`${mail.subject}\n${mail.text}`, { ref: mail.date ? new Date(mail.date) : now, now, lang: get(locale), chrono })
    const first = found[0]
    if (first) {
      draft.due = first.allDay ? dayKey(first.start) : first.start.toISOString()
    }
  } catch {
    draft.due = ''
  }
  return draft
}

/** Shows a task in the Tasks space. */
export function openTask(id: number): void {
  taskFocus.set(id)
  openSpace('tasks')
}

/**
 * Makes a task from each message and says so, with Undo and Open. Returns the
 * ids of the tasks made.
 */
export async function createTasksFromMail(messageIds: number[]): Promise<number[]> {
  const tr = get(t)
  if (get(taskLists).length === 0) {
    await loadTasks()
  }
  const listId = defaultListId(get(taskLists), get(taskSettings))
  if (listId === 0) {
    toastError(tr('tasks.noWritable'))
    return []
  }
  const made: number[] = []
  for (const id of messageIds) {
    try {
      const draft = await draftFromMail(await mailEventSource(id), listId)
      const result = await createTask(draft)
      made.push(result.task.id)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
  if (made.length === 0) {
    return made
  }
  await loadTasks()
  const message = made.length > 1 ? tr('tasks.fromMailMany').replace('{n}', String(made.length)) : tr('tasks.fromMail')
  pushActions(
    'success',
    message,
    [
      {
        label: tr('tasks.undo'),
        run: () =>
          void (async () => {
            for (const taskId of made) {
              await deleteTask(taskId).catch((err) => toastError(errorMessage(err)))
            }
            await loadTasks()
          })(),
      },
      { label: tr('tasks.open'), run: () => openTask(made[0]) },
    ],
    8000,
  )
  return made
}
