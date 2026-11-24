// legible.ts lifts the foreground tokens of a theme to wcag aa contrast against
// its surfaces for the new design. the built-in light and dark sets carry their
// lifted values in tokens.css; a theme pack gets them computed here, since its
// colors are only known at runtime.

import { contrastRatio, isValidHex, normalizeHex } from './accent'

/** The surface tokens text is drawn on. */
export const SURFACE_TOKENS = ['surface-base', 'surface-raised', 'surface-overlay', 'surface-sunken', 'surface-hover'] as const

/** The foreground tokens that carry text and must reach 4.5:1 on every surface. */
export const TEXT_TOKENS = ['text-secondary', 'text-tertiary', 'success', 'warning', 'danger'] as const

/** The built-in values a theme pack falls back to for any token it leaves out. */
export const BASE_TOKENS: Record<'light' | 'dark', Record<string, string>> = {
  light: {
    'surface-base': '#f6f6f7',
    'surface-raised': '#ffffff',
    'surface-overlay': '#ffffff',
    'surface-sunken': '#efeff1',
    'surface-hover': '#ececef',
    'text-primary': '#1a1b1e',
    'text-secondary': '#4a4d54',
    'text-tertiary': '#676a72',
    success: '#197a48',
    warning: '#916100',
    danger: '#c0392b',
  },
  dark: {
    'surface-base': '#111214',
    'surface-raised': '#1a1c1f',
    'surface-overlay': '#222428',
    'surface-sunken': '#161719',
    'surface-hover': '#25282d',
    'text-primary': '#ecedee',
    'text-secondary': '#b4b7bd',
    'text-tertiary': '#8b8f97',
    success: '#3fb27a',
    warning: '#d9a441',
    danger: '#e5675a',
  },
}

const minText = 4.5

function channels(hex: string): number[] {
  return [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16))
}

function mix(from: string, to: string, t: number): string {
  const a = channels(from)
  const b = channels(to)
  return '#' + a.map((v, i) => Math.round(v + (b[i] - v) * t).toString(16).padStart(2, '0')).join('')
}

/**
 * The color closest to fg, moving toward `toward`, that reaches 4.5:1 on every
 * surface. A color that already does is returned unchanged.
 */
export function legibleTone(fg: string, toward: string, surfaces: string[]): string {
  const from = normalizeHex(fg)
  const to = normalizeHex(toward)
  for (let step = 0; step <= 100; step++) {
    const tone = mix(from, to, step / 100)
    if (surfaces.every((s) => contrastRatio(tone, s) >= minText)) {
      return tone
    }
  }
  return to
}

/**
 * The text tokens of a theme pack lifted for the new design, by token name.
 * Tokens that are not plain hex colors, in the pack or its base, are skipped.
 */
export function legibleTokens(base: 'light' | 'dark', tokens: Record<string, string>): Record<string, string> {
  const value = (name: string): string | null => {
    const v = (tokens[name] ?? BASE_TOKENS[base][name] ?? '').trim()
    return isValidHex(v) ? normalizeHex(v) : null
  }
  const surfaces = SURFACE_TOKENS.map(value)
  const primary = value('text-primary')
  if (!primary || surfaces.some((s) => s === null)) {
    return {}
  }
  const out: Record<string, string> = {}
  for (const name of TEXT_TOKENS) {
    const fg = value(name)
    if (fg) {
      out[name] = legibleTone(fg, primary, surfaces as string[])
    }
  }
  return out
}
