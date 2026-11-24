// mailcolors.ts decides how html mail is coloured in a dark theme and does the
// adapting. Mail is sorted by how much it styles itself: plain mail sets no
// colours at all, lightly styled mail sets a few (a coloured word, a
// highlighted line), designed mail lays itself out with its own backgrounds,
// tables and pictures. Plain and lightly styled mail follows the theme;
// designed mail keeps its own colours on a light sheet, since recolouring a
// newsletter wrecks it. A choice made for the message or remembered for the
// sender overrides that, and so does the global setting.
//
// Adapting rewrites the colours the mail declares, one value at a time, by
// turning their lightness round while keeping their hue: dark text becomes
// light, a light background becomes dark or goes away. Nothing is inverted as
// a whole, so pictures are never touched.

import type { HtmlColors } from './types'

/** How much a message styles itself. */
export type MailStyle = 'plain' | 'light' | 'designed'

/** How a message is shown: adapted to the theme, or as sent. */
export type ColorMode = 'adapt' | 'original'

/** An rgb colour with alpha, channels 0 to 255 and alpha 0 to 1. */
export interface Rgba {
  r: number
  g: number
  b: number
  a: number
}

const named: Record<string, string> = {
  black: '#000000',
  white: '#ffffff',
  red: '#ff0000',
  green: '#008000',
  blue: '#0000ff',
  navy: '#000080',
  maroon: '#800000',
  purple: '#800080',
  teal: '#008080',
  olive: '#808000',
  gray: '#808080',
  grey: '#808080',
  silver: '#c0c0c0',
  lime: '#00ff00',
  yellow: '#ffff00',
  orange: '#ffa500',
  aqua: '#00ffff',
  fuchsia: '#ff00ff',
  darkblue: '#00008b',
  darkred: '#8b0000',
  darkgreen: '#006400',
  darkgray: '#a9a9a9',
  darkgrey: '#a9a9a9',
  dimgray: '#696969',
  dimgrey: '#696969',
  lightgray: '#d3d3d3',
  lightgrey: '#d3d3d3',
  gainsboro: '#dcdcdc',
  whitesmoke: '#f5f5f5',
  ivory: '#fffff0',
  beige: '#f5f5dc',
  lightyellow: '#ffffe0',
}

/** Reads a css colour: hex, rgb(), rgba() or a common name. Null for anything else. */
export function parseColor(value: string): Rgba | null {
  const v = value.trim().toLowerCase()
  if (v === 'transparent') {
    return { r: 0, g: 0, b: 0, a: 0 }
  }
  const hex = named[v] ?? v
  let m = /^#([0-9a-f]{3,8})$/.exec(hex)
  if (m) {
    const h = m[1]
    if (h.length === 3 || h.length === 4) {
      const [r, g, b, a] = [...h].map((c) => parseInt(c + c, 16))
      return { r, g, b, a: h.length === 4 ? a / 255 : 1 }
    }
    if (h.length === 6 || h.length === 8) {
      const p = (i: number): number => parseInt(h.slice(i, i + 2), 16)
      return { r: p(0), g: p(2), b: p(4), a: h.length === 8 ? p(6) / 255 : 1 }
    }
    return null
  }
  m = /^rgba?\(\s*([\d.]+)[\s,]+([\d.]+)[\s,]+([\d.]+)(?:[\s,/]+([\d.]+%?))?\s*\)$/.exec(v)
  if (m) {
    const alpha = m[4] === undefined ? 1 : m[4].endsWith('%') ? parseFloat(m[4]) / 100 : parseFloat(m[4])
    return { r: +m[1], g: +m[2], b: +m[3], a: Math.max(0, Math.min(1, alpha)) }
  }
  return null
}

function toHsl({ r, g, b }: Rgba): { h: number; s: number; l: number } {
  const [rr, gg, bb] = [r / 255, g / 255, b / 255]
  const max = Math.max(rr, gg, bb)
  const min = Math.min(rr, gg, bb)
  const l = (max + min) / 2
  if (max === min) {
    return { h: 0, s: 0, l }
  }
  const d = max - min
  const s = l > 0.5 ? d / (2 - max - min) : d / (max + min)
  let h: number
  if (max === rr) {
    h = (gg - bb) / d + (gg < bb ? 6 : 0)
  } else if (max === gg) {
    h = (bb - rr) / d + 2
  } else {
    h = (rr - gg) / d + 4
  }
  return { h: h / 6, s, l }
}

function fromHsl(h: number, s: number, l: number, a: number): Rgba {
  if (s === 0) {
    const v = Math.round(l * 255)
    return { r: v, g: v, b: v, a }
  }
  const q = l < 0.5 ? l * (1 + s) : l + s - l * s
  const p = 2 * l - q
  const hue = (t: number): number => {
    const x = t < 0 ? t + 1 : t > 1 ? t - 1 : t
    if (x < 1 / 6) return p + (q - p) * 6 * x
    if (x < 1 / 2) return q
    if (x < 2 / 3) return p + (q - p) * (2 / 3 - x) * 6
    return p
  }
  return { r: Math.round(hue(h + 1 / 3) * 255), g: Math.round(hue(h) * 255), b: Math.round(hue(h - 1 / 3) * 255), a }
}

function format(c: Rgba): string {
  return c.a >= 1 ? `rgb(${c.r}, ${c.g}, ${c.b})` : `rgba(${c.r}, ${c.g}, ${c.b}, ${+c.a.toFixed(3)})`
}

/** The lightness of a colour, 0 for black to 1 for white. */
export function lightness(c: Rgba): number {
  return toHsl(c).l
}

/**
 * Turns one colour round for a dark page. Text keeps its hue and becomes as
 * light as it was dark; text that is light already stays. A background that
 * is nearly white goes away so the page shows through, a lighter tint becomes
 * the matching dark tint, and a dark one stays.
 */
export function adaptColor(value: string, role: 'fg' | 'bg'): string {
  const c = parseColor(value)
  if (!c || c.a === 0) {
    return value
  }
  const { h, s, l } = toHsl(c)
  if (role === 'fg') {
    if (l >= 0.6) {
      return value
    }
    return format(fromHsl(h, Math.min(s, 0.75), 0.62 + (1 - l) * 0.3, c.a))
  }
  if (l >= 0.9) {
    return 'transparent'
  }
  if (l <= 0.35) {
    return value
  }
  return format(fromHsl(h, Math.min(s, 0.6), 0.1 + (1 - l) * 0.3, c.a))
}

const colorToken = /#[0-9a-f]{3,8}\b|rgba?\([^)]*\)|\b[a-z]+\b/gi

// adaptValue rewrites every colour inside one declaration's value.
function adaptValue(value: string, role: 'fg' | 'bg'): string {
  if (/url\(/i.test(value)) {
    return value
  }
  return value.replace(colorToken, (token) => (parseColor(token) ? adaptColor(token, role) : token))
}

const declaration = /(^|[;{\s])(color|background-color|background)(\s*:\s*)([^;}!]+)/gi

/** Adapts the colours in a run of css declarations, such as a style attribute. */
export function adaptDeclarations(css: string): string {
  return css.replace(declaration, (_all, lead: string, prop: string, sep: string, value: string) => {
    return `${lead}${prop}${sep}${adaptValue(value, prop.toLowerCase() === 'color' ? 'fg' : 'bg')}`
  })
}

/**
 * Rewrites the colours a sanitized html body declares for a dark page: style
 * attributes and the bgcolor and font color attributes, which is all the
 * sanitizer lets through. Pictures and everything else are left as they are.
 */
export function adaptHtml(html: string): string {
  const doc = new DOMParser().parseFromString(`<body>${html}</body>`, 'text/html')
  for (const el of Array.from(doc.body.querySelectorAll<HTMLElement>('*'))) {
    const style = el.getAttribute('style')
    if (style) {
      el.setAttribute('style', adaptDeclarations(style))
    }
    const bg = el.getAttribute('bgcolor')
    if (bg) {
      el.setAttribute('bgcolor', adaptColor(bg, 'bg'))
    }
    if (el.tagName === 'FONT') {
      const fg = el.getAttribute('color')
      if (fg) {
        el.setAttribute('color', adaptColor(fg, 'fg'))
      }
    }
  }
  return doc.body.innerHTML
}

// a background that is not white or see-through, which only a styled mail sets.
function painted(value: string): string | null {
  const c = parseColor(value)
  if (!c || c.a === 0 || lightness(c) >= 0.97) {
    return null
  }
  return `${c.r},${c.g},${c.b}`
}

// firstColor finds the colour in a value that may carry more, such as a
// background shorthand.
function firstColor(value: string): string {
  return value.match(colorToken)?.find((token) => parseColor(token) !== null) ?? ''
}

function pixels(value: string | null): number {
  const n = parseInt(value ?? '', 10)
  return Number.isNaN(n) ? 0 : n
}

/**
 * Sorts a sanitized html body by how much it styles itself (see the top of
 * this file). The signals are the backgrounds it paints, background pictures,
 * tables laid out in tables or at a fixed width, large or many pictures, and
 * the number of text colours. The sanitizer keeps no style sheets, so style
 * attributes, bgcolor and font colours are all there is to read.
 */
export function mailStyle(html: string): MailStyle {
  const doc = new DOMParser().parseFromString(`<body>${html}</body>`, 'text/html')
  const backgrounds = new Set<string>()
  const textColors = new Set<string>()
  let score = 0
  let colored = false
  for (const el of Array.from(doc.body.querySelectorAll<HTMLElement>('*'))) {
    const style = el.getAttribute('style') ?? ''
    if (/background(-image)?\s*:[^;]*url\(/i.test(style)) {
      return 'designed'
    }
    for (const m of style.matchAll(/(?:^|;)\s*(background-color|background|color)\s*:\s*([^;!]+)/gi)) {
      colored = true
      if (m[1].toLowerCase() === 'color') {
        textColors.add(m[2].trim().toLowerCase())
      } else {
        const key = painted(firstColor(m[2]))
        if (key) {
          backgrounds.add(key)
        }
      }
    }
    const bg = painted(el.getAttribute('bgcolor') ?? '')
    if (bg) {
      backgrounds.add(bg)
      colored = true
    }
    if (el.tagName === 'FONT' && el.hasAttribute('color')) {
      textColors.add((el.getAttribute('color') ?? '').toLowerCase())
      colored = true
    }
    if (el.tagName === 'TABLE') {
      if (el.parentElement?.closest('table')) {
        score += 2
      }
      if (pixels(el.getAttribute('width')) >= 500 || /(^|;)\s*width\s*:\s*([5-9]\d\d|\d{4,})px/i.test(style)) {
        score += 1
      }
    }
  }
  const images = Array.from(doc.querySelectorAll('img'))
  if (images.length >= 3) {
    score += 1
  }
  if (images.some((img) => pixels(img.getAttribute('width')) >= 300)) {
    score += 1
  }
  if (backgrounds.size === 1) {
    score += 1
  } else if (backgrounds.size >= 2) {
    score += 3
  }
  if (textColors.size >= 3) {
    score += 1
  }
  if (score === 0 && !colored) {
    return 'plain'
  }
  return score <= 2 ? 'light' : 'designed'
}

/** What decides a message's colours. */
export interface ColorChoice {
  /** The theme on screen is dark. */
  dark: boolean
  /** How much the message styles itself. */
  style: MailStyle
  /** The global setting. */
  pref: HtmlColors
  /** The choice remembered for the sender, or '' for none. */
  sender: '' | ColorMode
  /** The choice made for this message, or '' for none. */
  message: '' | ColorMode
}

/**
 * Picks how a message is shown. A light theme always shows mail as sent. In
 * a dark theme the message's own choice wins, then the sender's, then the
 * global setting, whose automatic mode adapts all but designed mail.
 */
export function colorMode(c: ColorChoice): ColorMode {
  if (!c.dark) {
    return 'original'
  }
  if (c.message !== '') {
    return c.message
  }
  if (c.sender !== '') {
    return c.sender
  }
  if (c.pref !== 'auto') {
    return c.pref
  }
  return c.style === 'designed' ? 'original' : 'adapt'
}

/** What the decision would be with neither a message nor a sender choice, for telling a choice apart from the default. */
export function defaultMode(c: Omit<ColorChoice, 'sender' | 'message'>): ColorMode {
  return colorMode({ ...c, sender: '', message: '' })
}
