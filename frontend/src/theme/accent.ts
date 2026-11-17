// accent.ts owns everything about the single user-chosen accent color: validating
// the hex the user types, computing a legible foreground for text on the accent
// surface, and injecting both into the css custom properties. every other accent
// derivation (selection background, dark-mode link) is done in tokens.css with
// color-mix so we only ever store one color.

import { writable } from 'svelte/store'

// preset swatches of the classic design, offered alongside the free hex input.
// neutral, calm choices that read well as a dezent selection/link accent in
// both themes.
export const ACCENT_PRESETS: readonly string[] = [
  '#465AF2', // pelton blue (default)
  '#5B6470', // graphite
  '#3A7D5D', // pine
  '#9A5B3A', // clay
  '#7A4FB5', // violet
  '#B5476A', // rose
  '#2E8C9E', // teal
  '#C28A1E', // amber
]

/** One accent of the new design: a tone for light themes and one for dark ones. */
export interface AccentSwatch {
  name: string
  /** The tone for light themes; also the value stored when it is picked. */
  light: string
  /** The tone for dark themes. */
  dark: string
}

// the accents of the new design. every light tone sits at oklch lightness 0.52
// and every dark tone at 0.75, with chroma near 0.11 and 0.10 (amber and teal a
// little lower to stay in gamut, graphite nearly grey), so no swatch is louder
// than another. light tones carry white text, dark tones black text.
export const NEW_ACCENTS: readonly AccentSwatch[] = [
  { name: 'blue', light: '#4567a8', dark: '#8baeee' },
  { name: 'indigo', light: '#635ea5', dark: '#a7a5eb' },
  { name: 'violet', light: '#7b5699', dark: '#bf9ddf' },
  { name: 'plum', light: '#924d7d', dark: '#d995c2' },
  { name: 'rose', light: '#9d4b5a', dark: '#e5939f' },
  { name: 'rust', light: '#9d5035', dark: '#e5987d' },
  { name: 'amber', light: '#8a5f18', dark: '#d3a563' },
  { name: 'green', light: '#317a45', dark: '#7fc08c' },
  { name: 'teal', light: '#117878', dark: '#61c0bf' },
  { name: 'graphite', light: '#5b6472', dark: '#aab2be' },
]

// the accent every install starts with. the new design shows it as its own
// default blue; the classic design keeps it as it is.
const startingAccent = '#465af2'

/** The preset swatches a design offers, as the values stored when picked. */
export function accentPresets(design: string): readonly string[] {
  return design === 'classic' ? ACCENT_PRESETS : NEW_ACCENTS.map((s) => s.light)
}

/**
 * The stored accent as the picker should see it: in the new design the
 * starting accent counts as the default blue. Anything else is returned as is.
 */
export function canonicalAccent(hex: string, design: string): string {
  const n = isValidHex(hex) ? normalizeHex(hex) : hex
  return design !== 'classic' && n === startingAccent ? NEW_ACCENTS[0].light : n
}

/**
 * The color the accent paints in: a new-design preset takes its tone for the
 * theme; a custom color, and every color in the classic design, stays as is.
 */
export function accentTone(hex: string, design: string, dark: boolean): string {
  const n = canonicalAccent(hex, design)
  const swatch = design === 'classic' ? undefined : NEW_ACCENTS.find((s) => s.light === n)
  return swatch ? (dark ? swatch.dark : swatch.light) : n
}

/** Whether the live theme is a dark one, for showing swatches in their live tone. */
export const darkScheme = writable(false)

// hexPattern accepts 3 or 6 digit hex with a leading hash.
const hexPattern = /^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/

// isValidHex reports whether s is a well formed #rgb or #rrggbb color. used by
// the accent input for live validation so invalid input is rejected gracefully
// rather than producing a broken theme.
export function isValidHex(s: string): boolean {
  return hexPattern.test(s.trim())
}

// normalizeHex expands a valid #rgb to #rrggbb and lowercases it. callers must
// pass a string that isValidHex already accepted.
export function normalizeHex(s: string): string {
  let hex = s.trim().toLowerCase()
  if (hex.length === 4) {
    // #rgb -> #rrggbb
    hex = '#' + hex[1] + hex[1] + hex[2] + hex[2] + hex[3] + hex[3]
  }
  return hex
}

// rgb is a parsed color in 0-255 channels.
interface RGB {
  r: number
  g: number
  b: number
}

// parseHex converts a normalized #rrggbb to channel values.
function parseHex(hex: string): RGB {
  const n = normalizeHex(hex)
  return {
    r: parseInt(n.slice(1, 3), 16),
    g: parseInt(n.slice(3, 5), 16),
    b: parseInt(n.slice(5, 7), 16),
  }
}

// relativeLuminance computes the wcag relative luminance of a color (0..1),
// linearizing each channel per the wcag 2.x definition.
function relativeLuminance({ r, g, b }: RGB): number {
  const channel = (c: number): number => {
    const s = c / 255
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4)
  }
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b)
}

/** The wcag contrast ratio of two #rrggbb colors, 1 to 21. */
export function contrastRatio(a: string, b: string): number {
  const x = relativeLuminance(parseHex(a))
  const y = relativeLuminance(parseHex(b))
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05)
}

// contrastForeground returns black or white, whichever has higher contrast on
// the given accent, so text on an accent surface stays legible for any hex the
// user picks. accent is used only for selection and links per the design, but we
// still guarantee legibility wherever text sits on the accent.
export function contrastForeground(hex: string): string {
  // contrast ratio of white vs black against the color decides the foreground.
  const lum = relativeLuminance(parseHex(hex))
  const contrastWithWhite = 1.05 / (lum + 0.05)
  const contrastWithBlack = (lum + 0.05) / 0.05
  return contrastWithWhite >= contrastWithBlack ? '#ffffff' : '#000000'
}

let chosen = ''
let watching = false

function paint(): void {
  const root = document.documentElement
  const dark = root.getAttribute('data-theme') === 'dark'
  darkScheme.set(dark)
  if (!chosen) {
    return
  }
  const tone = accentTone(chosen, root.getAttribute('data-design') ?? 'new', dark)
  root.style.setProperty('--accent', tone)
  root.style.setProperty('--accent-fg', contrastForeground(tone))
}

// applyAccent injects the chosen accent and its computed foreground into the
// root element, in the tone the design and theme call for, and repaints when
// either changes. the selection and link derivations follow automatically
// through the color-mix rules in tokens.css. invalid input is ignored so a half
// typed hex never blanks the theme.
export function applyAccent(hex: string): void {
  if (!isValidHex(hex)) {
    return
  }
  chosen = normalizeHex(hex)
  if (!watching && typeof MutationObserver !== 'undefined') {
    watching = true
    new MutationObserver(paint).observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme', 'data-design'] })
  }
  paint()
}
