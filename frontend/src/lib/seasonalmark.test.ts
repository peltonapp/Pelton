import { describe, expect, it } from 'vitest'
import { easterSunday, seasonalMark } from './seasonalmark'

describe('easterSunday', () => {
  it('matches known dates', () => {
    expect(easterSunday(2026)).toEqual([3, 5])
    expect(easterSunday(2027)).toEqual([2, 28])
    expect(easterSunday(2038)).toEqual([3, 25])
  })
})

describe('seasonalMark', () => {
  it('is none on an ordinary day', () => {
    expect(seasonalMark(new Date(2026, 10, 15))).toBeNull()
    expect(seasonalMark(new Date(2026, 11, 26))).toBeNull()
  })

  it('picks the fixed days', () => {
    expect(seasonalMark(new Date(2027, 3, 1))).toBe('april-first')
    expect(seasonalMark(new Date(2026, 9, 31))).toBe('halloween')
    expect(seasonalMark(new Date(2026, 11, 24))).toBe('christmas')
    expect(seasonalMark(new Date(2026, 11, 25, 23, 59))).toBe('christmas')
  })

  it('picks easter sunday and monday', () => {
    expect(seasonalMark(new Date(2027, 2, 28))).toBe('easter')
    expect(seasonalMark(new Date(2027, 2, 29))).toBe('easter')
    expect(seasonalMark(new Date(2027, 2, 30))).toBeNull()
    expect(seasonalMark(new Date(2027, 2, 27))).toBeNull()
  })
})
