// maildrop.ts says what dropping mail on the calendar will do, for the label
// that follows the dragged stack: a plain drop reads the dates in each mail,
// a Shift drop makes one event that links to the mails.

import { get } from 'svelte/store'
import { t } from '../../lib/i18n'

/**
 * The drag label for count messages over the calendar. onSlot is true over a
 * day or time slot, which a plain drop keeps instead of the mail's date.
 */
export function mailDropHint(count: number, shift: boolean, onSlot: boolean): string {
  const tr = get(t)
  let first: string
  if (shift) {
    first = count > 1 ? tr('calendar.drop.linkMany').replace('{n}', String(count)) : tr('calendar.drop.link')
  } else if (count > 1) {
    first = tr('calendar.drop.many').replace('{n}', String(count))
  } else {
    first = tr(onSlot ? 'calendar.drop.here' : 'calendar.drop.dates')
  }
  return `${first}\n${tr(shift ? 'calendar.drop.releaseShift' : 'calendar.drop.holdShift')}`
}
