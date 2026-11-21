import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import { get } from 'svelte/store'

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  isDevMode: vi.fn().mockResolvedValue(false),
  isNightly: vi.fn().mockResolvedValue(false),
  setSetting: vi.fn().mockResolvedValue(undefined),
}))

import StatusBar from './StatusBar.svelte'
import { sidebar } from '../../stores/accounts'
import { prefs } from '../../stores/prefs'
import { proposals } from '../../stores/agent'
import { selection } from '../../stores/selection'
import { ready, idle } from '../../lib/async'
import type { UnifiedView } from '../../lib/types'

function withInboxUnread(n: number): void {
  const views = [{ key: 'inbox', label: 'Unified Inbox', totalCount: 10, unreadCount: n }] as UnifiedView[]
  sidebar.set(ready({ accounts: [], foldersByAccount: {}, views, pinned: [] }))
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout', 'setInterval', 'clearInterval'] })
  vi.setSystemTime(new Date(2026, 9, 13, 15, 42, 30))
})

afterEach(() => {
  vi.useRealTimers()
  sidebar.set(idle())
  proposals.set([])
  prefs.update((p) => ({ ...p, statusClock: true, statusUnread: true, timeFormat: 'auto' }))
  selection.set({ kind: 'folder', folderId: 1, accountId: 1, label: 'x' } as never)
})

describe('status bar clock and unread count (#488)', () => {
  it('shows the time and moves it on at the next minute', async () => {
    prefs.update((p) => ({ ...p, timeFormat: '24' }))
    render(StatusBar)
    expect(document.querySelector('time.clock')?.textContent).toContain('15:42')

    await vi.advanceTimersByTimeAsync(30_000)
    expect(document.querySelector('time.clock')?.textContent).toContain('15:43')
  })

  it('shows the unified inbox unread count and opens the inbox on click', async () => {
    withInboxUnread(3)
    render(StatusBar)

    const button = screen.getByRole('button', { name: /3 unread/ })
    await fireEvent.click(button)
    expect(get(selection)).toMatchObject({ kind: 'view', view: 'inbox' })
  })

  it('leaves the count out when nothing is unread', () => {
    withInboxUnread(0)
    render(StatusBar)
    expect(screen.queryByRole('button', { name: /unread/ })).toBeNull()
  })

  it('shows waiting agent proposals with a single profile', () => {
    proposals.set([{ id: 1, accountId: 1, to: 'a@example.com', cc: '', bcc: '', subject: 's', body: 'b', when: '' }])
    render(StatusBar)
    expect(screen.getByRole('button', { name: /1 awaiting you/ })).toBeTruthy()
  })

  it('shows no proposal chip when nothing is waiting', () => {
    proposals.set([])
    render(StatusBar)
    expect(screen.queryByRole('button', { name: /awaiting you/ })).toBeNull()
  })

  it('hides both when they are switched off', () => {
    withInboxUnread(3)
    prefs.update((p) => ({ ...p, statusClock: false, statusUnread: false }))
    render(StatusBar)
    expect(document.querySelector('time.clock')).toBeNull()
    expect(screen.queryByRole('button', { name: /unread/ })).toBeNull()
  })
})
