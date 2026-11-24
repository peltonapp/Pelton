import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import StatusBarPreview from './StatusBarPreview.svelte'
import SidebarPreview from './SidebarPreview.svelte'
import TitleBarPreview from './TitleBarPreview.svelte'
import MenuBarPreview from './MenuBarPreview.svelte'
import PaneLockPreview from './PaneLockPreview.svelte'

describe('StatusBarPreview', () => {
  it('shows the clock and the unread count only when on', () => {
    const on = render(StatusBarPreview, { props: { clock: true, unread: true } })
    expect(on.container.querySelector('.clock')).not.toBeNull()
    expect(screen.queryByText('3 unread')).not.toBeNull()
    on.unmount()
    const off = render(StatusBarPreview, { props: { clock: false, unread: false } })
    expect(off.container.querySelector('.clock')).toBeNull()
    expect(screen.queryByText('3 unread')).toBeNull()
  })

  it('draws the progress bar only for a running sync with the bar on', () => {
    const idle = render(StatusBarPreview, { props: { progress: true } })
    expect(idle.container.querySelector('.meter')).toBeNull()
    idle.unmount()
    const bar = render(StatusBarPreview, { props: { syncing: true, progress: true } })
    expect(bar.container.querySelector('.meter')).not.toBeNull()
    bar.unmount()
    const none = render(StatusBarPreview, { props: { syncing: true, progress: false } })
    expect(none.container.querySelector('.meter')).toBeNull()
  })

  it('names the mailbox and server in a verbose sync', () => {
    render(StatusBarPreview, { props: { syncing: true, verbose: true } })
    expect(screen.getByText(/Syncing INBOX for grace@compiler.example on imap.compiler.example/)).toBeTruthy()
  })
})

describe('SidebarPreview', () => {
  it('draws indent guides only when on', () => {
    const on = render(SidebarPreview, { props: { indentGuides: true } })
    expect(on.container.querySelectorAll('.guide').length).toBeGreaterThan(0)
    on.unmount()
    const off = render(SidebarPreview, { props: { indentGuides: false } })
    expect(off.container.querySelectorAll('.guide')).toHaveLength(0)
  })

  it('writes the address or the name in the account header', () => {
    const email = render(SidebarPreview, { props: { accountEmail: true } })
    expect(screen.queryByText('grace@compiler.example')).not.toBeNull()
    email.unmount()
    render(SidebarPreview, { props: { accountEmail: false } })
    expect(screen.queryByText('Grace Hopper')).not.toBeNull()
  })

  it('counts Flagged only when on', () => {
    const on = render(SidebarPreview, { props: { views: 'sidebar', flaggedCount: true } })
    expect(on.container.querySelectorAll('.count')).toHaveLength(2)
    on.unmount()
    const off = render(SidebarPreview, { props: { views: 'sidebar', flaggedCount: false } })
    expect(off.container.querySelectorAll('.count')).toHaveLength(1)
  })

  it('marks the folder that is not syncing only when on', () => {
    const on = render(SidebarPreview, { props: { unsynced: true } })
    expect(on.container.querySelector('.off')).not.toBeNull()
    on.unmount()
    const off = render(SidebarPreview, { props: { unsynced: false } })
    expect(off.container.querySelector('.off')).toBeNull()
  })

  it('moves the unified views between the sidebar, a tab strip and nowhere', () => {
    const side = render(SidebarPreview, { props: { views: 'sidebar' } })
    expect(side.container.querySelector('.tabs')).toBeNull()
    expect(screen.queryByText('Unified')).not.toBeNull()
    side.unmount()
    const tab = render(SidebarPreview, { props: { views: 'tab' } })
    expect(tab.container.querySelector('.tabs')).not.toBeNull()
    expect(screen.queryByText('Unified')).toBeNull()
    tab.unmount()
    const hidden = render(SidebarPreview, { props: { views: 'hidden' } })
    expect(hidden.container.querySelector('.tabs')).toBeNull()
    expect(screen.queryByText('Unified')).toBeNull()
  })
})

describe('TitleBarPreview', () => {
  it('switches between the system bar and the integrated one', () => {
    const native = render(TitleBarPreview, { props: { native: true } })
    expect(native.container.querySelector('.window')?.classList.contains('native')).toBe(true)
    native.unmount()
    const integrated = render(TitleBarPreview, { props: { native: false } })
    expect(integrated.container.querySelector('.window')?.classList.contains('native')).toBe(false)
  })
})

describe('MenuBarPreview', () => {
  it('puts the menu in the window or in the system bar', () => {
    const inApp = render(MenuBarPreview, { props: { inApp: true } })
    expect(inApp.container.querySelector('.menu.system')).toBeNull()
    expect(inApp.container.querySelector('.window .menu')).not.toBeNull()
    inApp.unmount()
    const system = render(MenuBarPreview, { props: { inApp: false } })
    expect(system.container.querySelector('.menu.system')).not.toBeNull()
    expect(system.container.querySelector('.window .menu')).toBeNull()
  })

  it('adds an icon to each entry only when on', () => {
    const on = render(MenuBarPreview, { props: { icons: true } })
    expect(on.container.querySelectorAll('.entry svg')).toHaveLength(3)
    on.unmount()
    const off = render(MenuBarPreview, { props: { icons: false } })
    expect(off.container.querySelectorAll('.entry svg')).toHaveLength(0)
  })
})

describe('PaneLockPreview', () => {
  it('swaps the drag handle for a padlock', () => {
    const open = render(PaneLockPreview, { props: { locked: false } })
    const handle = open.container.querySelector('.grip')?.innerHTML
    open.unmount()
    const locked = render(PaneLockPreview, { props: { locked: true } })
    expect(locked.container.querySelector('.panes')?.classList.contains('locked')).toBe(true)
    expect(locked.container.querySelector('.grip')?.innerHTML).not.toBe(handle)
  })
})
