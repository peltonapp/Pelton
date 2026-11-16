// notetemplate.ts fills note templates and names daily notes. Dates use the
// moment-style tokens Obsidian users already know (YYYY-MM-DD, dddd, ...),
// with text in [brackets] kept as written. Templates are ordinary notes
// whose {{variables}} are replaced when a note is made from them; a variable
// nothing provides stays as it is, so a template meant for another tool is
// not mangled.

const tokenRe = /\[([^\]]*)\]|YYYY|YY|MMMM|MMM|MM|M|DDDD|DD|Do|D|dddd|ddd|dd|HH|H|hh|h|mm|m|ss|s|A|a|ww|w|Q/g

function pad(n: number, width = 2): string {
  return String(n).padStart(width, '0')
}

function ordinal(n: number): string {
  const rem = n % 100
  if (rem >= 11 && rem <= 13) {
    return `${n}th`
  }
  switch (n % 10) {
    case 1:
      return `${n}st`
    case 2:
      return `${n}nd`
    case 3:
      return `${n}rd`
  }
  return `${n}th`
}

function isoWeek(d: Date): number {
  const t = new Date(Date.UTC(d.getFullYear(), d.getMonth(), d.getDate()))
  const day = t.getUTCDay() || 7
  t.setUTCDate(t.getUTCDate() + 4 - day)
  const start = new Date(Date.UTC(t.getUTCFullYear(), 0, 1))
  return Math.ceil(((t.getTime() - start.getTime()) / 86400000 + 1) / 7)
}

function dayOfYear(d: Date): number {
  return Math.round((new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime() - new Date(d.getFullYear(), 0, 1).getTime()) / 86400000) + 1
}

/** Formats a date with moment-style tokens; locale names months and weekdays. */
export function formatNoteDate(date: Date, format: string, locale = 'en'): string {
  const month = (style: 'long' | 'short'): string => date.toLocaleDateString(locale, { month: style })
  const weekday = (style: 'long' | 'short' | 'narrow'): string => date.toLocaleDateString(locale, { weekday: style })
  const h = date.getHours()
  return format.replace(tokenRe, (tok, literal: string | undefined) => {
    if (literal !== undefined) {
      return literal
    }
    switch (tok) {
      case 'YYYY':
        return String(date.getFullYear())
      case 'YY':
        return pad(date.getFullYear() % 100)
      case 'MMMM':
        return month('long')
      case 'MMM':
        return month('short')
      case 'MM':
        return pad(date.getMonth() + 1)
      case 'M':
        return String(date.getMonth() + 1)
      case 'DDDD':
        return pad(dayOfYear(date), 3)
      case 'DD':
        return pad(date.getDate())
      case 'D':
        return String(date.getDate())
      case 'Do':
        return ordinal(date.getDate())
      case 'dddd':
        return weekday('long')
      case 'ddd':
        return weekday('short')
      case 'dd':
        return weekday('narrow')
      case 'HH':
        return pad(h)
      case 'H':
        return String(h)
      case 'hh':
        return pad(h % 12 || 12)
      case 'h':
        return String(h % 12 || 12)
      case 'mm':
        return pad(date.getMinutes())
      case 'm':
        return String(date.getMinutes())
      case 'ss':
        return pad(date.getSeconds())
      case 's':
        return String(date.getSeconds())
      case 'A':
        return h < 12 ? 'AM' : 'PM'
      case 'a':
        return h < 12 ? 'am' : 'pm'
      case 'ww':
        return pad(isoWeek(date))
      case 'w':
        return String(isoWeek(date))
      case 'Q':
        return String(Math.floor(date.getMonth() / 3) + 1)
    }
    return tok
  })
}

/** The default daily note format. */
export const defaultDailyFormat = 'YYYY-MM-DD'

/** The vault path of the daily note for a day. */
export function dailyNotePath(date: Date, folder: string, format: string, locale = 'en'): string {
  const name = formatNoteDate(date, format.trim() || defaultDailyFormat, locale)
  const f = folder.trim().replace(/^\/+|\/+$/g, '')
  return `${f ? `${f}/` : ''}${name}.md`
}

/**
 * The day a daily note's path names, when it is in the daily folder and its
 * name matches the format exactly; null otherwise. Only formats made of
 * numeric date tokens can be read back.
 */
export function dailyNoteDate(path: string, folder: string, format: string): Date | null {
  const f = folder.trim().replace(/^\/+|\/+$/g, '')
  if (f && !path.startsWith(`${f}/`)) {
    return null
  }
  const name = path.slice(f ? f.length + 1 : 0).replace(/\.md$/i, '')
  const parts: string[] = []
  let re = '^'
  let last = 0
  const fmt = format.trim() || defaultDailyFormat
  for (const m of fmt.matchAll(tokenRe)) {
    re += escapeRe(fmt.slice(last, m.index))
    last = (m.index ?? 0) + m[0].length
    if (m[1] !== undefined) {
      re += escapeRe(m[1])
      continue
    }
    switch (m[0]) {
      case 'YYYY':
        re += '(\\d{4})'
        parts.push('Y')
        break
      case 'MM':
        re += '(\\d{2})'
        parts.push('M')
        break
      case 'M':
        re += '(\\d{1,2})'
        parts.push('M')
        break
      case 'DD':
        re += '(\\d{2})'
        parts.push('D')
        break
      case 'D':
        re += '(\\d{1,2})'
        parts.push('D')
        break
      default:
        re += '(.+?)'
        parts.push('')
    }
  }
  re += `${escapeRe(fmt.slice(last))}$`
  const m = new RegExp(re).exec(name)
  if (!m || !parts.includes('Y') || !parts.includes('M') || !parts.includes('D')) {
    return null
  }
  const get = (k: string): number => Number(m[parts.indexOf(k) + 1])
  const d = new Date(get('Y'), get('M') - 1, get('D'))
  return dailyNotePath(d, f, fmt) === path ? d : null
}

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/** An event a meeting note is about. start and end are display text. */
export interface TemplateEvent {
  title: string
  start: string
  end: string
  location: string
  attendees: string[]
  link: string
}

/** What a template can be filled with. */
export interface TemplateVars {
  date: Date
  title: string
  locale?: string
  /** Default formats of {{date}} and {{time}}. */
  dateFormat?: string
  timeFormat?: string
  event?: TemplateEvent
  /** A Markdown link to the Pelton object a note is about. */
  link?: string
  /** The day's events and due tasks, as Markdown list lines. */
  events?: string[]
  tasks?: string[]
}

const varRe = /\{\{\s*([a-zA-Z.]+)(?::([^}]*))?\s*\}\}/g

/** Fills a template's {{variables}}. Unknown ones stay as written. */
export function expandTemplate(template: string, vars: TemplateVars): string {
  const locale = vars.locale ?? 'en'
  return template.replace(varRe, (whole, name: string, arg: string | undefined) => {
    const fmt = arg?.trim()
    switch (name) {
      case 'date':
        return formatNoteDate(vars.date, fmt || vars.dateFormat || defaultDailyFormat, locale)
      case 'time':
        return formatNoteDate(vars.date, fmt || vars.timeFormat || 'HH:mm', locale)
      case 'title':
        return vars.title
      case 'link':
        return vars.link ?? whole
      case 'events':
        return vars.events ? listOrNone(vars.events) : whole
      case 'tasks':
        return vars.tasks ? listOrNone(vars.tasks) : whole
      case 'event.title':
        return vars.event ? vars.event.title : whole
      case 'event.start':
        return vars.event ? vars.event.start : whole
      case 'event.end':
        return vars.event ? vars.event.end : whole
      case 'event.location':
        return vars.event ? vars.event.location : whole
      case 'event.attendees':
        return vars.event ? vars.event.attendees.map((a) => `- ${a}`).join('\n') : whole
      case 'event.link':
        return vars.event ? vars.event.link : whole
    }
    return whole
  })
}

function listOrNone(lines: string[]): string {
  return lines.length ? lines.join('\n') : '-'
}

/** The template a daily note starts from when none is chosen. */
export const builtinDailyTemplate = '# {{date:dddd, D MMMM YYYY}}\n\n## Agenda\n{{events}}\n\n## Due\n{{tasks}}\n\n## Notes\n\n'

/** The template a meeting note starts from when the templates folder has no "Meeting" note. */
export const builtinMeetingTemplate = '# {{event.title}}\n\n{{event.link}}\n**When:** {{event.start}}\n**Attendees:**\n{{event.attendees}}\n\n## Notes\n\n\n## Actions\n- [ ] \n'

/** The template a note about a mail, contact or task starts from when none is chosen. */
export const builtinAboutTemplate = '# {{title}}\n\n{{link}}\n\n'
