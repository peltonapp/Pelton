import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/svelte'
import MarkdownPreview from './MarkdownPreview.svelte'

describe('MarkdownPreview', () => {
  const hostile = '\n\nOn x wrote:\n> hi\n> <img src=x onerror="window.__pwned=1">\n'

  it('never puts rendered markdown into the app document', () => {
    const { container } = render(MarkdownPreview, { markdown: hostile, font: '' })
    expect(container.querySelector('img')).toBeNull()
    const frame = container.querySelector('iframe')
    expect(frame).not.toBeNull()
  })

  it('renders into a frame that cannot run script', () => {
    const { container } = render(MarkdownPreview, { markdown: hostile, font: '' })
    const frame = container.querySelector('iframe') as HTMLIFrameElement
    expect(frame.getAttribute('sandbox')).toBe('')
    const srcdoc = frame.getAttribute('srcdoc') ?? ''
    expect(srcdoc).toContain("script-src 'none'")
    expect(srcdoc).toContain('<blockquote>')
  })

  it('sends link clicks to a new window instead of navigating the frame', () => {
    const { container } = render(MarkdownPreview, { markdown: '[x](https://example.com)', font: '' })
    const srcdoc = container.querySelector('iframe')?.getAttribute('srcdoc') ?? ''
    const base = srcdoc.indexOf('<base target="_blank">')
    expect(base).toBeGreaterThan(-1)
    expect(base).toBeLessThan(srcdoc.indexOf('<body>'))
  })

  it('drops a quoted link or form target that would override the base target', () => {
    const markdown = '> <a target="_self" href="https://x">x</a> <form target="_top"><button formtarget="_self">go</button></form>'
    const { container } = render(MarkdownPreview, { markdown, font: '' })
    const srcdoc = container.querySelector('iframe')?.getAttribute('srcdoc') ?? ''
    expect(srcdoc).toContain('href="https://x"')
    expect(srcdoc).not.toMatch(/\btarget="_self"|target="_top"|formtarget=/)
  })

  it('renders each quoted source line on its own line', () => {
    const { container } = render(MarkdownPreview, { markdown: '> Update your information\n> Hi Jan,\n> Follow these steps', font: '' })
    const doc = new DOMParser().parseFromString(container.querySelector('iframe')!.srcdoc, 'text/html')
    expect(doc.querySelectorAll('blockquote br')).toHaveLength(2)
  })
})

describe('MarkdownPreview theme', () => {
  const root = document.documentElement
  const originalStyle = root.getAttribute('style')
  const originalTheme = root.getAttribute('data-theme')
  afterEach(() => {
    if (originalStyle === null) root.removeAttribute('style')
    else root.setAttribute('style', originalStyle)
    if (originalTheme === null) root.removeAttribute('data-theme')
    else root.setAttribute('data-theme', originalTheme)
  })

  it('paints the composer surface and uses the dark color scheme inside the frame', () => {
    root.setAttribute('data-theme', 'dark')
    root.style.setProperty('--surface-overlay', '#202123')
    root.style.setProperty('--text-primary', '#eeeeee')
    const { container } = render(MarkdownPreview, { markdown: '> reply', font: '' })
    const srcdoc = container.querySelector('iframe')!.srcdoc
    expect(srcdoc).toContain('background:#202123')
    expect(srcdoc).toContain('color:#eeeeee')
    expect(srcdoc).toContain('color-scheme:dark')
  })

  it('updates an open preview when the theme changes without editing the message', async () => {
    root.setAttribute('data-theme', 'dark')
    root.style.setProperty('--surface-overlay', '#202123')
    root.style.setProperty('--text-primary', '#eeeeee')
    const { container } = render(MarkdownPreview, { markdown: 'Unchanged body', font: '' })
    root.setAttribute('data-theme', 'light')
    root.style.setProperty('--surface-overlay', '#ffffff')
    root.style.setProperty('--text-primary', '#222222')
    await vi.waitFor(() => {
      const srcdoc = container.querySelector('iframe')!.srcdoc
      expect(srcdoc).toContain('background:#ffffff')
      expect(srcdoc).toContain('color:#222222')
      expect(srcdoc).toContain('color-scheme:light')
    })
  })
})
