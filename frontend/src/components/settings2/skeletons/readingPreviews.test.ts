import { afterEach, describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import { get } from 'svelte/store'
import { prefs } from '../../../stores/prefs'
import SenderFontPreview from './SenderFontPreview.svelte'
import BadgesPreview from './BadgesPreview.svelte'
import OpeningPreview from './OpeningPreview.svelte'
import RemoteContentPreview from './RemoteContentPreview.svelte'
import EmptyStatePreview from './EmptyStatePreview.svelte'
import ShortcutHintPreview from './ShortcutHintPreview.svelte'
import PalettePreview from './PalettePreview.svelte'
import AppVimPreview from './AppVimPreview.svelte'

const saved = get(prefs)

afterEach(() => {
  prefs.set(saved)
})

describe('SenderFontPreview', () => {
  it('keeps the sender typeface only when on', () => {
    const on = render(SenderFontPreview, { props: { senderFonts: true } })
    expect(on.container.querySelector('.body')?.classList.contains('designed')).toBe(true)
    on.unmount()
    const off = render(SenderFontPreview, { props: { senderFonts: false } })
    expect(off.container.querySelector('.body')?.classList.contains('designed')).toBe(false)
  })
})

describe('BadgesPreview', () => {
  it('shows each badge only while its own setting is on', () => {
    prefs.update((p) => ({ ...p, showMailboxBadge: true, showPgp: false, showAuth: false }))
    const { container, unmount } = render(BadgesPreview)
    expect(container.querySelectorAll('.badge')).toHaveLength(1)
    unmount()
    prefs.update((p) => ({ ...p, showMailboxBadge: true, showPgp: true, showAuth: true }))
    const all = render(BadgesPreview)
    expect(all.container.querySelectorAll('.badge')).toHaveLength(3)
    all.unmount()
    prefs.update((p) => ({ ...p, showMailboxBadge: false, showPgp: false, showAuth: false }))
    const none = render(BadgesPreview)
    expect(none.container.querySelectorAll('.badge')).toHaveLength(0)
  })
})

describe('OpeningPreview', () => {
  it('keeps the unread dot until opening marks the message read', () => {
    const keep = render(OpeningPreview, { props: { markRead: false, openOnArrow: true } })
    expect(keep.container.querySelectorAll('.dot.on')).toHaveLength(1)
    keep.unmount()
    const read = render(OpeningPreview, { props: { markRead: true, openOnArrow: true } })
    expect(read.container.querySelectorAll('.dot.on')).toHaveLength(0)
  })

  it('shows the highlighted message in the pane only with open on arrow', () => {
    const on = render(OpeningPreview, { props: { openOnArrow: true } })
    expect(on.container.querySelector('.title')?.textContent).toBe('Compiler release schedule')
    on.unmount()
    const off = render(OpeningPreview, { props: { openOnArrow: false } })
    expect(off.container.querySelector('.title')?.textContent).toBe('Notes on the Analytical Engine')
  })

  it('never marks the unopened message read', () => {
    const { container } = render(OpeningPreview, { props: { markRead: true, openOnArrow: false } })
    expect(container.querySelectorAll('.dot.on')).toHaveLength(1)
  })
})

describe('RemoteContentPreview', () => {
  it('says images are blocked until they load', () => {
    const blocked = render(RemoteContentPreview, { props: { images: false } })
    expect(screen.queryByText('Remote images are blocked to protect your privacy.')).not.toBeNull()
    expect(blocked.container.querySelector('.picture')?.classList.contains('blocked')).toBe(true)
    blocked.unmount()
    const loaded = render(RemoteContentPreview, { props: { images: true } })
    expect(screen.queryByText('Remote images are blocked to protect your privacy.')).toBeNull()
    expect(loaded.container.querySelector('.picture')?.classList.contains('blocked')).toBe(false)
  })

  it('lets the tracking pixel through only with the block off', () => {
    const kept = render(RemoteContentPreview, { props: { images: true, blockPixels: true } })
    expect(kept.container.querySelector('.pixel')?.classList.contains('loaded')).toBe(false)
    expect(screen.queryByText(/tracking pixels stayed blocked/)).not.toBeNull()
    kept.unmount()
    const open = render(RemoteContentPreview, { props: { images: true, blockPixels: false } })
    expect(open.container.querySelector('.pixel')?.classList.contains('loaded')).toBe(true)
    expect(screen.queryByText(/tracking pixels stayed blocked/)).toBeNull()
  })
})

describe('EmptyStatePreview', () => {
  it('fills the pane only in full screen', () => {
    const small = render(EmptyStatePreview, { props: { src: 'a.png', fullscreen: false } })
    expect(small.container.querySelector('img')?.classList.contains('fill')).toBe(false)
    small.unmount()
    const full = render(EmptyStatePreview, { props: { src: 'a.png', fullscreen: true } })
    expect(full.container.querySelector('img')?.classList.contains('fill')).toBe(true)
  })
})

describe('ShortcutHintPreview', () => {
  it('shows a key beside each action only when on', () => {
    const on = render(ShortcutHintPreview, { props: { hints: true } })
    expect(on.container.querySelectorAll('kbd')).toHaveLength(3)
    on.unmount()
    const off = render(ShortcutHintPreview, { props: { hints: false } })
    expect(off.container.querySelectorAll('kbd')).toHaveLength(0)
  })
})

describe('PalettePreview', () => {
  it('shows the quick select keys only when on', () => {
    const on = render(PalettePreview, { props: { quickSelect: true } })
    expect(on.container.querySelectorAll('kbd')).toHaveLength(3)
    on.unmount()
    const off = render(PalettePreview, { props: { quickSelect: false } })
    expect(off.container.querySelectorAll('kbd')).toHaveLength(0)
  })
})

describe('AppVimPreview', () => {
  it('moves the highlight and lights the keys only when on', () => {
    const on = render(AppVimPreview, { props: { enabled: true } })
    expect(on.container.querySelector('.list')?.classList.contains('enabled')).toBe(true)
    expect(on.container.querySelector('.keys')?.classList.contains('off')).toBe(false)
    on.unmount()
    const off = render(AppVimPreview, { props: { enabled: false } })
    expect(off.container.querySelector('.list')?.classList.contains('enabled')).toBe(false)
    expect(off.container.querySelector('.keys')?.classList.contains('off')).toBe(true)
  })
})
