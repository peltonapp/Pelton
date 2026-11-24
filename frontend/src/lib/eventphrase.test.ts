import { describe, expect, it } from 'vitest'
import * as en from 'chrono-node/en'
import * as de from 'chrono-node/de'
import { dateMentions, datesInText, parsePhrase, type ChronoLike, type PhraseResult } from './eventphrase'

// Saturday 14 November 2026, 10:07.
const now = new Date(2026, 10, 14, 10, 7)
const english: ChronoLike[] = [en.casual]
const german: ChronoLike[] = [de.casual, en.casual]

function read(text: string, lang = 'en', extra: Partial<Parameters<typeof parsePhrase>[1]> = {}): PhraseResult {
  return parsePhrase(text, { now, base: now, lang, chrono: lang === 'de' ? german : english, ...extra })
}

const at = (d: number, h: number, min = 0, month = 10, year = 2026): Date => new Date(year, month, d, h, min)

describe('parsePhrase', () => {
  const cases: { text: string; lang?: string; want: Partial<PhraseResult> }[] = [
    {
      text: 'lunch with Ada tomorrow 12:30 for 1h at Cafe Blau',
      want: { title: 'lunch with Ada', start: at(15, 12, 30), end: at(15, 13, 30), allDay: false, location: 'Cafe Blau', dated: true },
    },
    { text: 'dentist fri 9am', want: { title: 'dentist', start: at(20, 9), end: at(20, 10), allDay: false } },
    {
      text: 'team offsite 3 to 5 March',
      want: { title: 'team offsite', start: new Date(2027, 2, 3), end: new Date(2027, 2, 6), allDay: true },
    },
    {
      // Saturday: the first weekday is Monday.
      text: 'standup every weekday 9:15',
      want: { title: 'standup', start: at(16, 9, 15), end: at(16, 10, 15), rrule: 'FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR' },
    },
    {
      text: 'call mum in 2 hours remind me 10 min before',
      want: { title: 'call mum', start: at(14, 12, 7), reminderMinutes: 10 },
    },
    { text: 'review 10:00-11:30 tomorrow', want: { title: 'review', start: at(15, 10), end: at(15, 11, 30) } },
    { text: 'yoga every monday 7am', want: { title: 'yoga', start: at(16, 7), rrule: 'FREQ=WEEKLY' } },
    { text: 'gym mondays and thursdays 18:00', want: { title: 'gym', rrule: 'FREQ=WEEKLY;BYDAY=MO,TH' } },
    { text: 'rent due monthly from 1 December', want: { title: 'rent due', rrule: 'FREQ=MONTHLY', start: new Date(2026, 11, 1), allDay: true } },
    { text: 'conference all day next friday', want: { title: 'conference', allDay: true, start: new Date(2026, 10, 20), end: new Date(2026, 10, 21) } },
    { text: 'holiday tomorrow for 3 days', want: { title: 'holiday', allDay: true, start: new Date(2026, 10, 15), end: new Date(2026, 10, 18) } },
    { text: 'Mittagessen mit Ada morgen um 12:30 für 90 min im Café Blau', lang: 'de', want: { title: 'Mittagessen mit Ada', start: at(15, 12, 30), end: at(15, 14), location: 'Café Blau' } },
    { text: 'Zahnarzt Fr 9 Uhr', lang: 'de', want: { title: 'Zahnarzt', start: at(20, 9) } },
    { text: 'Stand-up jeden Werktag 9:15 Erinnerung 5 min vorher', lang: 'de', want: { title: 'Stand-up', rrule: 'FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR', reminderMinutes: 5, start: at(16, 9, 15) } },
    { text: 'Termin 20.12. 14:00', lang: 'de', want: { title: 'Termin', start: at(20, 14, 0, 11) } },
    { text: 'Urlaub ganztägig 2.1.2027', lang: 'de', want: { title: 'Urlaub', allDay: true, start: new Date(2027, 0, 2) } },
    { text: 'Sport montags 18 Uhr', lang: 'de', want: { title: 'Sport', rrule: 'FREQ=WEEKLY', start: at(16, 18) } },
    { text: 'Yoga alle 2 Wochen', lang: 'de', want: { title: 'Yoga', rrule: 'FREQ=WEEKLY;INTERVAL=2' } },
  ]
  for (const c of cases) {
    it(c.text, () => {
      expect(read(c.text, c.lang ?? 'en')).toMatchObject(c.want)
    })
  }

  it('says when nothing datelike was found and uses the next half hour', () => {
    const r = read('buy milk')
    expect(r).toMatchObject({ title: 'buy milk', dated: false, start: at(14, 10, 30), end: at(14, 11, 30), allDay: false })
  })

  it('reads a time against a clicked day and slot', () => {
    const base = new Date(2026, 10, 25)
    expect(read('lunch 12:30', 'en', { base })).toMatchObject({ start: at(25, 12, 30) })
    expect(read('lunch', 'en', { base, baseMinutes: 14 * 60 })).toMatchObject({ start: at(25, 14), dated: false })
    // a day with no time keeps the clicked slot's time
    expect(read('lunch tomorrow', 'en', { base, baseMinutes: 14 * 60 })).toMatchObject({ start: at(26, 14), allDay: false })
  })

  it('keeps the default slot on another day inside that day', () => {
    const late = new Date(2026, 10, 14, 23, 40)
    const r = parsePhrase('plan', { now: late, base: new Date(2026, 10, 20), lang: 'en', chrono: english })
    expect(r.start).toEqual(at(20, 23))
    const today = parsePhrase('plan', { now: late, base: late, lang: 'en', chrono: english })
    expect(today.start).toEqual(at(15, 0))
  })

  it('lists what it recognized in order', () => {
    const r = read('lunch tomorrow 12:30 for 1h at Cafe Blau remind me 15 min before')
    expect(r.parts.map((p) => [p.kind, p.text])).toEqual([
      ['date', 'tomorrow 12:30'],
      ['duration', 'for 1h'],
      ['location', 'at Cafe Blau'],
      ['reminder', 'remind me 15 min before'],
    ])
  })

  it('falls back to the English table for a language without one', () => {
    expect(read('dentist fri 9am', 'pl')).toMatchObject({ title: 'dentist', start: at(20, 9) })
  })
})

describe('datesInText', () => {
  const sent = new Date(2026, 10, 7, 9)

  it('finds a date with a time in a mail', () => {
    const found = datesInText("Hi,\nLet's meet up on the 3rd December 2026 at 18:00. Cheers", { ref: sent, now, lang: 'en', chrono: english })
    expect(found[0]).toMatchObject({ start: new Date(2026, 11, 3, 18), end: new Date(2026, 11, 3, 19), allDay: false })
  })

  it('reads relative dates against when the mail was sent', () => {
    const found = datesInText('Can we talk tomorrow at 3pm?', { ref: sent, now, lang: 'en', chrono: english })
    expect(found[0].start).toEqual(new Date(2026, 10, 8, 15))
  })

  it('puts dates with a time first, then future ones', () => {
    const found = datesInText('The report from 2 November is late. Party on 20 November. Call on 25 November at 10:00.', { ref: sent, now, lang: 'en', chrono: english })
    expect(found.map((f) => f.start)).toEqual([new Date(2026, 10, 25, 10), new Date(2026, 10, 20), new Date(2026, 10, 2)])
    expect(found[1].allDay).toBe(true)
  })

  it('takes a place named after the date', () => {
    const found = datesInText('Dinner on Friday at 19:00 at Cafe Blau, see you', { ref: sent, now, lang: 'en', chrono: english })
    expect(found[0].location).toBe('Cafe Blau')
  })

  it('finds nothing in a text without dates', () => {
    expect(datesInText('Thanks for the files.', { ref: sent, now, lang: 'en', chrono: english })).toEqual([])
  })
})

describe('dateMentions', () => {
  // Saturday 7 November 2026, 09:00.
  const sent = new Date(2026, 10, 7, 9)

  it('gives each date with where it stands in the text', () => {
    const text = 'Can we meet Thursday at 3? If not, 12 March, 14:00 to 15:30 works too.'
    const found = dateMentions(text, { ref: sent, chrono: english })
    expect(found.map((m) => text.slice(m.index, m.index + m.length))).toEqual(['Thursday at 3', '12 March, 14:00 to 15:30'])
    expect(found[0]).toMatchObject({ start: new Date(2026, 10, 12, 15), end: new Date(2026, 10, 12, 16), allDay: false })
    expect(found[1]).toMatchObject({ start: new Date(2027, 2, 12, 14), end: new Date(2027, 2, 12, 15, 30), allDay: false })
  })

  it('reads a day without a time as the whole day', () => {
    const text = 'The deadline is Friday.'
    const [m] = dateMentions(text, { ref: sent, chrono: english })
    expect(text.slice(m.index, m.index + m.length)).toBe('Friday')
    expect(m).toMatchObject({ start: new Date(2026, 10, 13), end: new Date(2026, 10, 14), allDay: true })
  })

  it('keeps every mention, the same date twice included, in text order', () => {
    const text = 'Friday works. See you Friday!'
    const found = dateMentions(text, { ref: sent, chrono: english })
    expect(found.map((m) => m.index)).toEqual([0, text.lastIndexOf('Friday')])
  })

  it('keeps a morning hour written with a leading zero, or with am', () => {
    expect(dateMentions('Thursday at 07:30', { ref: sent, chrono: english })[0].start).toEqual(new Date(2026, 10, 12, 7, 30))
    expect(dateMentions('Thursday at 7am', { ref: sent, chrono: english })[0].start).toEqual(new Date(2026, 10, 12, 7))
  })

  it('leaves out "now" and very short matches', () => {
    expect(dateMentions('Call me now please', { ref: sent, chrono: english })).toEqual([])
  })

  it('finds nothing in a text without dates', () => {
    expect(dateMentions('Thanks for the files.', { ref: sent, chrono: english })).toEqual([])
  })
})
