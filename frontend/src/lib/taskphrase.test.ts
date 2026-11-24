import { describe, expect, it } from 'vitest'
import * as en from 'chrono-node/en'
import { parseTaskPhrase } from './taskphrase'

// Sunday 15 November 2026, 10:07.
const now = new Date(2026, 10, 15, 10, 7)
const read = (text: string, defaultDueMinutes = -1): ReturnType<typeof parseTaskPhrase> =>
  parseTaskPhrase(text, { now, lang: 'en', chrono: [en.casual], defaultDueMinutes })

describe('parseTaskPhrase', () => {
  it('reads a day and a time', () => {
    const r = read('call Ada tomorrow 3pm')
    expect(r.title).toBe('call Ada')
    const due = new Date(r.due)
    expect([due.getDate(), due.getHours(), due.getMinutes()]).toEqual([16, 15, 0])
  })

  it('keeps a bare day a date', () => {
    expect(read('buy milk tomorrow')).toMatchObject({ title: 'buy milk', due: '2026-11-16' })
  })

  it('gives a bare day the default due time when one is set', () => {
    const due = new Date(read('buy milk tomorrow', 17 * 60).due)
    expect([due.getDate(), due.getHours()]).toEqual([16, 17])
  })

  it('reads a monthly repeat on a day of the month', () => {
    const r = read('pay rent every month on the 1st')
    expect(r.title).toBe('pay rent')
    expect(r.rrule).toBe('FREQ=MONTHLY')
    expect(r.due).toBe('2026-12-01')
  })

  it('leaves a line without a day undated', () => {
    expect(read('water the plants')).toMatchObject({ title: 'water the plants', due: '', rrule: '' })
  })
})
