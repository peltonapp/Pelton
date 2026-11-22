// timeblockmail.ts blocks time to deal with mail without leaving where the
// user is: dropped on the Calendar entry with Alt held, or from the command
// palette and its shortcut. Each mail gets a block in the next free time, or
// from a given time on, linked back to its conversation; a toast says when
// and offers Undo and Show.

import { get } from 'svelte/store'
import { blockTimeForMessages, deleteTimeBlocks } from './api'
import { formatWhen } from './calformat'
import { t } from './i18n'
import { calendarFocus, calendarView } from '../stores/calendar'
import { prefs } from '../stores/prefs'
import { openSpace } from '../stores/space'
import { errorMessage, pushActions, toastError } from '../stores/toast'
import type { TimeFormat } from './format'
import type { TimeBlock } from './types'

const toastMs = 8000

/** Blocks time for each mail and says so. Returns the blocks, or an empty list when it failed. */
export async function blockTimeForMail(messageIds: number[], at = ''): Promise<TimeBlock[]> {
  if (messageIds.length === 0) {
    return []
  }
  const tr = get(t)
  let made: TimeBlock[]
  try {
    made = await blockTimeForMessages(messageIds, at)
  } catch (err) {
    toastError(errorMessage(err))
    return []
  }
  if (made.length === 0) {
    return made
  }
  const first = made[0]
  const start = new Date(first.start)
  const when = formatWhen(start, new Date(first.end), false, get(prefs).timeFormat as TimeFormat)
  const message = made.length > 1 ? tr('timeblock.toast.madeMany').replace('{n}', String(made.length)) : tr('timeblock.toast.made').replace('{when}', when)
  pushActions(
    'success',
    message,
    [
      { label: tr('timeblock.toast.undo'), run: () => void deleteTimeBlocks(made.map((b) => b.id)).catch((err) => toastError(errorMessage(err))) },
      {
        label: tr('timeblock.toast.show'),
        run: () => {
          calendarFocus.set(start)
          calendarView.set('day')
          openSpace('calendar')
        },
      },
    ],
    toastMs,
  )
  return made
}
