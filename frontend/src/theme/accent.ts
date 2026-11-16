// accent.ts owns everything about the single user-chosen accent color: validating
// the hex the user types, computing a legible foreground for text on the accent
// surface, and injecting both into the css custom properties. every other accent
// derivation (selection background, dark-mode link) is done in tokens.css with
// color-mix so we only ever store one color.

// preset swatches offered alongside the free hex input. neutral, calm choices
// that read well as a dezent selection/link accent in both themes.
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

// applyAccent injects the chosen accent and its computed foreground into the
// root element. the selection and link derivations follow automatically through
// the color-mix rules in tokens.css. invalid input is ignored so a half typed
// hex never blanks the theme.
export function applyAccent(hex: string): void {
  if (!isValidHex(hex)) {
    return
  }
  const normalized = normalizeHex(hex)
  const root = document.documentElement
  root.style.setProperty('--accent', normalized)
  root.style.setProperty('--accent-fg', contrastForeground(normalized))
}
