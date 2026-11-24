import { describe, expect, it } from 'vitest'
import { adaptColor, adaptDeclarations, adaptHtml, colorMode, defaultMode, lightness, mailStyle, parseColor } from './mailcolors'

describe('parseColor', () => {
  it('reads hex, rgb and names', () => {
    expect(parseColor('#fff')).toEqual({ r: 255, g: 255, b: 255, a: 1 })
    expect(parseColor('#1a2b3c')).toEqual({ r: 26, g: 43, b: 60, a: 1 })
    expect(parseColor('rgba(10, 20, 30, 0.5)')).toEqual({ r: 10, g: 20, b: 30, a: 0.5 })
    expect(parseColor('rgb(10 20 30 / 50%)')).toEqual({ r: 10, g: 20, b: 30, a: 0.5 })
    expect(parseColor('Navy')).toEqual({ r: 0, g: 0, b: 128, a: 1 })
    expect(parseColor('transparent')?.a).toBe(0)
  })

  it('gives up on anything else', () => {
    expect(parseColor('solid')).toBeNull()
    expect(parseColor('var(--x)')).toBeNull()
  })
})

describe('adaptColor', () => {
  it('turns dark text light and keeps its hue', () => {
    const black = parseColor(adaptColor('#000000', 'fg'))!
    expect(lightness(black)).toBeGreaterThan(0.85)
    const navy = parseColor(adaptColor('navy', 'fg'))!
    expect(navy.b).toBeGreaterThan(navy.r)
    expect(lightness(navy)).toBeGreaterThan(0.6)
  })

  it('leaves light text alone', () => {
    expect(adaptColor('#eeeeee', 'fg')).toBe('#eeeeee')
  })

  it('drops a white background and darkens a light tint', () => {
    expect(adaptColor('#ffffff', 'bg')).toBe('transparent')
    const tint = parseColor(adaptColor('#ffee99', 'bg'))!
    expect(lightness(tint)).toBeLessThan(0.3)
    expect(tint.r).toBeGreaterThan(tint.b)
  })

  it('keeps a background that is already dark', () => {
    expect(adaptColor('#202020', 'bg')).toBe('#202020')
  })
})

describe('adaptDeclarations', () => {
  it('rewrites colour declarations and nothing else', () => {
    const out = adaptDeclarations('color: #000; border-color: #000; background-color: #fff; font-weight: bold')
    expect(out).toContain('border-color: #000')
    expect(out).toContain('background-color: transparent')
    expect(out).toContain('font-weight: bold')
    expect(out).not.toMatch(/(^|; )color: #000/)
  })

  it('leaves a background picture alone', () => {
    const css = 'background: #fff url(cid:a) no-repeat'
    expect(adaptDeclarations(css)).toBe(css)
  })

  it('reads declarations written without spaces', () => {
    expect(adaptDeclarations('font-size:12px;color:#111')).not.toContain('#111')
  })
})

describe('adaptHtml', () => {
  it('rewrites attributes and styles but never touches pictures', () => {
    const html = '<table bgcolor="#ffffff"><tr><td><font color="#000000">Hi</font><img src="data:image/png;base64,AA" style="width:10px"></td></tr></table>'
    const out = adaptHtml(html)
    expect(out).toContain('bgcolor="transparent"')
    expect(out).not.toContain('color="#000000"')
    expect(out).toContain('<img src="data:image/png;base64,AA" style="width:10px">')
    expect(out).not.toMatch(/filter|invert/)
  })
})

describe('mailStyle', () => {
  it('calls mail without colours plain', () => {
    expect(mailStyle('<p>Hello <b>there</b></p><blockquote>quoted</blockquote>')).toBe('plain')
  })

  it('calls a few coloured words light', () => {
    expect(mailStyle('<p>Hello <span style="color:#c00">urgent</span></p>')).toBe('light')
    expect(mailStyle('<p style="background-color:#ffff00">highlighted</p>')).toBe('light')
  })

  it('calls a laid out newsletter designed', () => {
    const html = `<table width="600" bgcolor="#f4f4f4"><tr><td><table bgcolor="#ffffff"><tr><td style="background-color:#0a3d62;color:#fff">Header</td></tr></table>
      <img src="a.png" width="600"></td></tr></table>`
    expect(mailStyle(html)).toBe('designed')
  })

  it('calls a background picture designed at once', () => {
    expect(mailStyle('<div style="background-image:url(cid:x)">hi</div>')).toBe('designed')
    expect(mailStyle('<div style="background: #fff url(cid:x) no-repeat">hi</div>')).toBe('designed')
  })

  it('reads a background colour out of the shorthand', () => {
    expect(mailStyle('<div style="background: rgb(240, 200, 10) none">a</div><div style="background-color:#123456">b</div>')).toBe('designed')
  })
})

describe('colorMode', () => {
  const base = { dark: true, style: 'light' as const, pref: 'auto' as const, sender: '' as const, message: '' as const }

  it('shows everything as sent in a light theme', () => {
    expect(colorMode({ ...base, dark: false, message: 'adapt' })).toBe('original')
  })

  it('adapts plain and light mail and keeps designed mail by default', () => {
    expect(colorMode({ ...base, style: 'plain' })).toBe('adapt')
    expect(colorMode(base)).toBe('adapt')
    expect(colorMode({ ...base, style: 'designed' })).toBe('original')
  })

  it('follows the global setting when it is not automatic', () => {
    expect(colorMode({ ...base, style: 'designed', pref: 'adapt' })).toBe('adapt')
    expect(colorMode({ ...base, pref: 'original' })).toBe('original')
  })

  it('lets the sender override the setting, and the message override the sender', () => {
    expect(colorMode({ ...base, style: 'designed', sender: 'adapt' })).toBe('adapt')
    expect(colorMode({ ...base, sender: 'adapt', message: 'original' })).toBe('original')
  })

  it('names the default apart from any choice', () => {
    expect(defaultMode({ dark: true, style: 'designed', pref: 'auto' })).toBe('original')
  })
})
