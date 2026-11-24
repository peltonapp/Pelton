// maildrop.ts says what dropping mail on the calendar will do, for the label
// that follows the dragged stack: a plain drop reads the dates in each mail,
// a Shift drop makes one event that links to the mails, and an Alt (Option)
// drop blocks time to deal with them.

import { get } from 'svelte/store'
import { t } from '../../lib/i18n'
import { isMac } from '../../lib/platform'

/**
 * The drag label for count messages over the calendar. onSlot is true over a
 * day or time slot, which a plain drop keeps instead of the mail's date.
 */
export function mailDropHint(count: number, shift: boolean, onSlot: boolean, alt = false): string {
  const tr = get(t)
  const altKey = isMac ? 'Option' : 'Alt'
  if (alt) {
    const first = count > 1 ? tr('calendar.drop.blockMany').replace('{n}', String(count)) : tr(onSlot ? 'calendar.drop.blockHere' : 'calendar.drop.block')
    return `${first}\n${tr('calendar.drop.releaseAlt').replace('{key}', altKey)}`
  }
  let first: string
  if (shift) {
    first = count > 1 ? tr('calendar.drop.linkMany').replace('{n}', String(count)) : tr('calendar.drop.link')
  } else if (count > 1) {
    first = tr('calendar.drop.many').replace('{n}', String(count))
  } else {
    first = tr(onSlot ? 'calendar.drop.here' : 'calendar.drop.dates')
  }
  return `${first}\n${tr(shift ? 'calendar.drop.releaseShift' : 'calendar.drop.holdShift')}\n${tr('calendar.drop.holdAlt').replace('{key}', altKey)}`
}
