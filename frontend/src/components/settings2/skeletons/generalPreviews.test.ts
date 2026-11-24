import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import SelectionPreview from './SelectionPreview.svelte'
import ThreadPreview from './ThreadPreview.svelte'
import DesktopPreview from './DesktopPreview.svelte'
import ToastPreview from './ToastPreview.svelte'
import TimeFormatPreview from './TimeFormatPreview.svelte'
import TabsPreview from './TabsPreview.svelte'
import CursorPreview from './CursorPreview.svelte'

describe('SelectionPreview', () => {
  it('has checkboxes and a header only with multi select on', () => {
    const on = render(SelectionPreview, { props: { enabled: true } })
    expect(on.container.querySelectorAll('.row .box')).toHaveLength(3)
    expect(on.container.querySelector('.head')).not.toBeNull()
    on.unmount()
    const off = render(SelectionPreview, { props: { enabled: false } })
    expect(off.container.querySelectorAll('.box')).toHaveLength(0)
    expect(off.container.querySelector('.head')).toBeNull()
  })

  it('counts the picked rows only when asked to', () => {
    const on = render(SelectionPreview, { props: { showCount: true } })
    expect(screen.queryByText('2 selected')).not.toBeNull()
    on.unmount()
    render(SelectionPreview, { props: { showCount: false } })
    expect(screen.queryByText('2 selected')).toBeNull()
  })

  it('says what select all reached for each scope', () => {
    const offer = render(SelectionPreview, { props: { scope: 'offer' } })
    expect(screen.queryByText(/All 50 loaded messages are selected/)).not.toBeNull()
    expect(screen.queryByText('Select all 240 in this list')).not.toBeNull()
    offer.unmount()
    const loaded = render(SelectionPreview, { props: { scope: 'loaded' } })
    expect(screen.queryByText(/All 50 loaded messages are selected/)).not.toBeNull()
    expect(screen.queryByText('Select all 240 in this list')).toBeNull()
    loaded.unmount()
    render(SelectionPreview, { props: { scope: 'all' } })
    expect(screen.queryByText('All 240 messages in the list are selected.')).not.toBeNull()
  })
})

describe('ThreadPreview', () => {
  it('folds a conversation into one row with its replies beneath', () => {
    const grouped = render(ThreadPreview, { props: { threaded: true } })
    expect(grouped.container.querySelector('.count')?.textContent).toBe('3')
    expect(grouped.container.querySelectorAll('.reply')).toHaveLength(2)
    grouped.unmount()
    const flat = render(ThreadPreview, { props: { threaded: false } })
    expect(flat.container.querySelector('.count')).toBeNull()
    expect(flat.container.querySelectorAll('.row')).toHaveLength(4)
  })
})

describe('DesktopPreview', () => {
  it('draws the window, the tray icon and the banner on request', () => {
    const all = render(DesktopPreview, { props: { appWindow: true, tray: true, banner: true } })
    expect(all.container.querySelector('.app-window')).not.toBeNull()
    expect(all.container.querySelector('.tray .app')).not.toBeNull()
    expect(all.container.querySelector('.banner')).not.toBeNull()
    all.unmount()
    const none = render(DesktopPreview, { props: { appWindow: false, tray: false, banner: false } })
    expect(none.container.querySelector('.app-window')).toBeNull()
    expect(none.container.querySelector('.tray .app')).toBeNull()
    expect(none.container.querySelector('.banner')).toBeNull()
  })
})

describe('ToastPreview', () => {
  it('pins the toast to the chosen position', () => {
    const left = render(ToastPreview, { props: { position: 'top-left' } })
    expect(left.container.querySelector('.toast')?.classList.contains('top-left')).toBe(true)
    left.unmount()
    const center = render(ToastPreview, { props: { position: 'bottom-center' } })
    expect(center.container.querySelector('.toast')?.classList.contains('bottom-center')).toBe(true)
  })
})

describe('TimeFormatPreview', () => {
  it('writes the time on a 24 hour or a 12 hour clock', () => {
    const h24 = render(TimeFormatPreview, { props: { format: '24' } })
    expect(h24.container.querySelector('.time')?.textContent).toContain('14:05')
    h24.unmount()
    const h12 = render(TimeFormatPreview, { props: { format: '12' } })
    const text = h12.container.querySelector('.time')?.textContent ?? ''
    expect(text).not.toContain('14:05')
    expect(text).toMatch(/2:05/)
  })
})

describe('TabsPreview', () => {
  it('brings back the earlier tabs only when restoring', () => {
    const on = render(TabsPreview, { props: { restore: true } })
    expect(on.container.querySelectorAll('.tab')).toHaveLength(4)
    on.unmount()
    const off = render(TabsPreview, { props: { restore: false } })
    expect(off.container.querySelectorAll('.tab')).toHaveLength(1)
  })
})

describe('CursorPreview', () => {
  it('swaps the arrow for the hand', () => {
    const arrow = render(CursorPreview, { props: { hand: false } })
    const arrowSvg = arrow.container.querySelector('.pointer')?.innerHTML
    arrow.unmount()
    const hand = render(CursorPreview, { props: { hand: true } })
    expect(hand.container.querySelector('.pointer')?.innerHTML).not.toBe(arrowSvg)
  })
})
