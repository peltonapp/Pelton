import { describe, expect, it } from 'vitest'
import { builtinDailyTemplate, dailyNoteDate, dailyNotePath, expandTemplate, formatNoteDate } from './notetemplate'

// Monday 16 November 2026, 09:05:07.
const day = new Date(2026, 10, 16, 9, 5, 7)

describe('formatNoteDate', () => {
  it('formats the common tokens', () => {
    expect(formatNoteDate(day, 'YYYY-MM-DD')).toBe('2026-11-16')
    expect(formatNoteDate(day, 'YY/M/D')).toBe('26/11/16')
    expect(formatNoteDate(day, 'HH:mm:ss')).toBe('09:05:07')
    expect(formatNoteDate(day, 'h:mm a')).toBe('9:05 am')
    expect(formatNoteDate(day, 'Do')).toBe('16th')
    expect(formatNoteDate(new Date(2026, 0, 1), 'Do [of] MMMM')).toBe('1st of January')
    expect(formatNoteDate(day, 'dddd')).toBe('Monday')
    expect(formatNoteDate(day, '[Week] ww, Q')).toBe('Week 47, 4')
    expect(formatNoteDate(day, 'DDDD')).toBe('320')
  })
})

describe('daily note paths', () => {
  it('builds the path from the folder and format', () => {
    expect(dailyNotePath(day, 'Daily', 'YYYY-MM-DD')).toBe('Daily/2026-11-16.md')
    expect(dailyNotePath(day, '/Journal/', 'YYYY/MM/DD')).toBe('Journal/2026/11/16.md')
    expect(dailyNotePath(day, '', '')).toBe('2026-11-16.md')
  })

  it('reads the day back from a path, and only from a matching one', () => {
    expect(dailyNoteDate('Daily/2026-11-16.md', 'Daily', 'YYYY-MM-DD')?.getDate()).toBe(16)
    expect(dailyNoteDate('Journal/2026/11/16.md', 'Journal', 'YYYY/MM/DD')?.getMonth()).toBe(10)
    expect(dailyNoteDate('Daily/2026-11-31.md', 'Daily', 'YYYY-MM-DD')).toBeNull()
    expect(dailyNoteDate('Other/2026-11-16.md', 'Daily', 'YYYY-MM-DD')).toBeNull()
    expect(dailyNoteDate('Daily/notes.md', 'Daily', 'YYYY-MM-DD')).toBeNull()
  })
})

describe('expandTemplate', () => {
  it('fills the variables and keeps unknown ones', () => {
    const out = expandTemplate('# {{title}} on {{date}} at {{time}} {{date:dddd}} {{unknown}} {{ link }}', {
      date: day,
      title: 'Plan',
      link: '[x](pelton://task/1)',
    })
    expect(out).toBe('# Plan on 2026-11-16 at 09:05 Monday {{unknown}} [x](pelton://task/1)')
  })

  it('fills a meeting from its event', () => {
    const out = expandTemplate('{{event.title}}|{{event.start}}|{{event.location}}\n{{event.attendees}}', {
      date: day,
      title: 'x',
      event: { title: 'Sync', start: 'Mon 10:00', end: '', location: 'Room 1', attendees: ['Ann', 'Bob'], link: '' },
    })
    expect(out).toBe('Sync|Mon 10:00|Room 1\n- Ann\n- Bob')
  })

  it('leaves event variables alone without an event', () => {
    expect(expandTemplate('{{event.title}}', { date: day, title: 'x' })).toBe('{{event.title}}')
  })

  it('lists the agenda in the built-in daily note, or a dash when empty', () => {
    const out = expandTemplate(builtinDailyTemplate, { date: day, title: '2026-11-16', events: ['- 10:00 [Call](pelton://event/u)'], tasks: [] })
    expect(out).toContain('# Monday, 16 November 2026')
    expect(out).toContain('## Agenda\n- 10:00 [Call](pelton://event/u)')
    expect(out).toContain('## Due\n-\n')
  })
})
