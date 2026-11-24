// seasonalmark.ts says which mark the About page shows on a given day. most
// days it is none, and the page shows the normal logo.

export type SeasonalMark = 'april-first' | 'easter' | 'halloween' | 'christmas'

/** Easter Sunday of a year in the Gregorian calendar, as [month index, day]. */
export function easterSunday(year: number): [number, number] {
  const a = year % 19
  const b = Math.floor(year / 100)
  const c = year % 100
  const d = Math.floor(b / 4)
  const e = b % 4
  const f = Math.floor((b + 8) / 25)
  const g = Math.floor((b - f + 1) / 3)
  const h = (19 * a + b - d - g + 15) % 30
  const i = Math.floor(c / 4)
  const k = c % 4
  const l = (32 + 2 * e + 2 * i - h - k) % 7
  const m = Math.floor((a + 11 * h + 22 * l) / 451)
  const month = Math.floor((h + l - 7 * m + 114) / 31) - 1
  const day = ((h + l - 7 * m + 114) % 31) + 1
  return [month, day]
}

/** The mark for a day, or null on an ordinary one. */
export function seasonalMark(date: Date): SeasonalMark | null {
  const month = date.getMonth()
  const day = date.getDate()
  if (month === 3 && day === 1) {
    return 'april-first'
  }
  if (month === 11 && (day === 24 || day === 25)) {
    return 'christmas'
  }
  if (month === 9 && day === 31) {
    return 'halloween'
  }
  // easter sunday and the monday after it.
  const [easterMonth, easterDay] = easterSunday(date.getFullYear())
  const sunday = new Date(date.getFullYear(), easterMonth, easterDay)
  const days = Math.round((new Date(date.getFullYear(), month, day).getTime() - sunday.getTime()) / 86_400_000)
  if (days === 0 || days === 1) {
    return 'easter'
  }
  return null
}
