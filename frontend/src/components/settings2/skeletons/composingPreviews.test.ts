import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import CaretPreview from './CaretPreview.svelte'
import RecipientsPreview from './RecipientsPreview.svelte'
import AutocompletePreview from './AutocompletePreview.svelte'
import SignaturePreview from './SignaturePreview.svelte'
import MarkdownPreview from './MarkdownPreview.svelte'
import SendDelayPreview from './SendDelayPreview.svelte'

vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

afterEach(() => {
  vi.useRealTimers()
  document.documentElement.removeAttribute('data-reduce-motion')
})

describe('CaretPreview', () => {
  it('lets the caret glide only with smooth typing on', () => {
    const on = render(CaretPreview, { props: { smooth: true } })
    expect(on.container.querySelector('.caret')?.classList.contains('glide')).toBe(true)
    on.unmount()
    const off = render(CaretPreview, { props: { smooth: false } })
    expect(off.container.querySelector('.caret')?.classList.contains('glide')).toBe(false)
  })

  it('types the sample one character at a time', async () => {
    vi.useFakeTimers()
    const { container } = render(CaretPreview, { props: { smooth: true } })
    const text = container.querySelector('.text')
    expect(text?.textContent).toBe('')
    await vi.advanceTimersByTimeAsync(130 * 3)
    expect(text?.textContent?.length).toBe(3)
  })

  it('draws the whole line at once with reduced motion', () => {
    document.documentElement.setAttribute('data-reduce-motion', '')
    const { container } = render(CaretPreview, { props: { smooth: true } })
    expect(container.querySelector('.text')?.textContent).toBe('Thanks for the notes, see you Friday.')
  })
})

describe('RecipientsPreview', () => {
  it('draws chips or comma separated text', () => {
    const chips = render(RecipientsPreview, { props: { chips: true } })
    expect(chips.container.querySelectorAll('.chip')).toHaveLength(2)
    chips.unmount()
    const plain = render(RecipientsPreview, { props: { chips: false } })
    expect(plain.container.querySelectorAll('.chip')).toHaveLength(0)
    expect(plain.container.querySelector('.plain')?.textContent).toContain('ada@analytical.example, grace@compiler.example')
  })
})

describe('AutocompletePreview', () => {
  it('shows suggestions only when enabled', () => {
    const on = render(AutocompletePreview, { props: { enabled: true } })
    expect(on.container.querySelectorAll('li')).toHaveLength(2)
    on.unmount()
    const off = render(AutocompletePreview, { props: { enabled: false } })
    expect(off.container.querySelectorAll('li')).toHaveLength(0)
  })
})

describe('SignaturePreview', () => {
  it('moves the signature above the quote', () => {
    const above = render(SignaturePreview, { props: { above: true } })
    expect(above.container.querySelector('.reply')?.classList.contains('above')).toBe(true)
    above.unmount()
    const below = render(SignaturePreview, { props: { above: false } })
    expect(below.container.querySelector('.reply')?.classList.contains('above')).toBe(false)
  })
})

describe('MarkdownPreview', () => {
  it('formats the marks only when on', () => {
    const on = render(MarkdownPreview, { props: { on: true } })
    expect(on.container.querySelector('strong')).not.toBeNull()
    expect(on.container.querySelector('.raw')).toBeNull()
    on.unmount()
    const off = render(MarkdownPreview, { props: { on: false } })
    expect(off.container.querySelector('strong')).toBeNull()
    expect(off.container.querySelector('.raw')?.textContent).toContain('**')
  })
})

describe('SendDelayPreview', () => {
  it('offers undo while the window is on', () => {
    render(SendDelayPreview, { props: { seconds: 10 } })
    expect(screen.queryByText('Undo')).not.toBeNull()
    expect(screen.queryByText('10s')).not.toBeNull()
  })

  it('has nothing to undo with the window off', () => {
    render(SendDelayPreview, { props: { seconds: 0 } })
    expect(screen.queryByText('Undo')).toBeNull()
  })
})
