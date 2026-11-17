import { describe, expect, it } from 'vitest'
import { ACCENT_PRESETS, NEW_ACCENTS, accentPresets, accentTone, canonicalAccent, contrastForeground, contrastRatio } from './accent'

// the surfaces of the built-in light and dark themes and of every bundled
// theme pack (internal/themepack/presets), by the base they build on.
const lightSurfaces = ['#ffffff', '#f6f6f7', '#efeff1', '#ececef', '#f4f1ea', '#fcfaf5', '#ece8de', '#eae6da', '#f5efdc', '#fdf6e3', '#eee8d5', '#ece5d0']
const darkSurfaces = [
  '#111214', '#1a1c1f', '#222428', '#161719', '#25282d',
  '#282828', '#32302f', '#3c3836', '#1d2021', '#403c3a',
  '#000000', '#0e0e11', '#17171c', '#060608', '#1c1c22',
  '#2e3440', '#3b4252', '#434c5e', '#272c36', '#404a5c',
  '#002b36', '#073642', '#0b3f4d', '#00232c', '#0e4250',
]

describe('new design accents', () => {
  it('offers 8 to 10 swatches', () => {
    expect(NEW_ACCENTS.length).toBeGreaterThanOrEqual(8)
    expect(NEW_ACCENTS.length).toBeLessThanOrEqual(10)
  })

  for (const s of NEW_ACCENTS) {
    it(`${s.name} keeps text on buttons at 4.5:1 and reads at 3:1 on every surface`, () => {
      expect(contrastRatio(s.light, contrastForeground(s.light))).toBeGreaterThanOrEqual(4.5)
      expect(contrastRatio(s.dark, contrastForeground(s.dark))).toBeGreaterThanOrEqual(4.5)
      for (const surface of lightSurfaces) {
        expect(contrastRatio(s.light, surface), `${s.light} on ${surface}`).toBeGreaterThanOrEqual(3)
      }
      for (const surface of darkSurfaces) {
        expect(contrastRatio(s.dark, surface), `${s.dark} on ${surface}`).toBeGreaterThanOrEqual(3)
      }
    })
  }

  it('paints a preset in its tone for the theme', () => {
    const blue = NEW_ACCENTS[0]
    expect(accentTone(blue.light, 'new', false)).toBe(blue.light)
    expect(accentTone(blue.light, 'new', true)).toBe(blue.dark)
  })

  it('shows the starting accent as the default blue in the new design only', () => {
    expect(canonicalAccent('#465AF2', 'new')).toBe(NEW_ACCENTS[0].light)
    expect(accentTone('#465AF2', 'new', true)).toBe(NEW_ACCENTS[0].dark)
    expect(canonicalAccent('#465AF2', 'classic')).toBe('#465af2')
    expect(accentTone('#465AF2', 'classic', true)).toBe('#465af2')
  })

  it('keeps any other stored color unchanged as a custom color', () => {
    expect(accentTone('#C28A1E', 'new', true)).toBe('#c28a1e')
    expect(accentTone('#123456', 'new', false)).toBe('#123456')
  })

  it('leaves the classic list as it was', () => {
    expect(accentPresets('classic')).toBe(ACCENT_PRESETS)
    expect(ACCENT_PRESETS).toEqual(['#465AF2', '#5B6470', '#3A7D5D', '#9A5B3A', '#7A4FB5', '#B5476A', '#2E8C9E', '#C28A1E'])
    expect(accentPresets('new')).toEqual(NEW_ACCENTS.map((s) => s.light))
  })
})
