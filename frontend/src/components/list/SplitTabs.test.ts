import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  splitCounts: vi.fn(),
  setSetting: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  splitCounts: api.splitCounts,
  setSetting: api.setSetting,
}))

import SplitTabs from './SplitTabs.svelte'
import { prefs } from '../../stores/prefs'
import { views } from '../../stores/views'
import { activeSplit, splitKeys, stepSplit } from '../../stores/splits'
import type { Selection, View } from '../../lib/types'

const inbox: Selection = { kind: 'view', view: 'inbox', label: 'Unified Inbox' }

function savedView(id: number, name: string): View {
  return {
    id,
    name,
    icon: '',
    color: '',
    queryText: '',
    queryFrom: [],
    queryTo: [],
    querySubject: '',
    withinDays: 0,
    useRegex: false,
    unreadOnly: false,
    flaggedOnly: false,
    hasAttachment: false,
    accountId: 0,
    position: 0,
    unreadCount: 0,
    totalCount: 0,
  } as View
}

beforeEach(() => {
  api.splitCounts.mockReset()
  api.setSetting.mockReset()
  api.setSetting.mockResolvedValue(undefined)
  api.splitCounts.mockResolvedValue([
    { key: 'important', total: 4, unread: 3 },
    { key: 'newsletters', total: 9, unread: 0 },
    { key: 'other', total: 2, unread: 1 },
  ])
  views.set([])
  prefs.update((p) => ({ ...p, design: 'new', splitInbox: true, splitTabs: 'important,newsletters', splitActive: '' }))
})

describe('split tab order', () => {
  it('keeps known tabs once, drops missing views and ends with Other', () => {
    const list = [savedView(4, 'Clients')]
    expect(splitKeys('newsletters,view:4,bogus,view:9,newsletters', list)).toEqual(['newsletters', 'view:4', 'other'])
    expect(activeSplit('view:9', ['important', 'other'])).toBe('important')
  })

  it('steps through the tabs and wraps around', () => {
    expect(stepSplit(1)).toBe('newsletters')
    expect(stepSplit(1)).toBe('other')
    expect(stepSplit(1)).toBe('important')
    expect(stepSplit(-1)).toBe('other')
    expect(get(prefs).splitActive).toBe('other')
  })
})

describe('SplitTabs', () => {
  it('shows the tabs in order with their unread counts', async () => {
    render(SplitTabs, { props: { selection: inbox } })
    const tabs = screen.getAllByRole('tab')
    expect(tabs.map((tab) => tab.textContent?.replace(/\s+/g, ' ').trim())).toEqual(['Important', 'Newsletters', 'Other'])
    expect(tabs[0]).toHaveAttribute('aria-selected', 'true')
    await waitFor(() => expect(within(tabs[0]).getByLabelText('3 unread')).toBeInTheDocument())
    expect(within(tabs[1]).queryByText('0')).not.toBeInTheDocument()
    expect(api.splitCounts).toHaveBeenCalledWith(inbox)
  })

  it('asks for the tab that was clicked', async () => {
    const user = userEvent.setup()
    const select = vi.fn()
    render(SplitTabs, { props: { selection: inbox }, events: { select: (e: CustomEvent<string>) => select(e.detail) } })
    await user.click(screen.getByRole('tab', { name: /Newsletters/ }))
    expect(select).toHaveBeenCalledWith('newsletters')
  })

  it('marks the remembered tab as selected', () => {
    prefs.update((p) => ({ ...p, splitActive: 'other' }))
    render(SplitTabs, { props: { selection: inbox } })
    expect(screen.getByRole('tab', { name: /Other/ })).toHaveAttribute('aria-selected', 'true')
  })

  it('edits which splits show and their order', async () => {
    const user = userEvent.setup()
    views.set([savedView(4, 'Clients')])
    render(SplitTabs, { props: { selection: inbox } })
    await user.click(screen.getByRole('button', { name: 'Edit splits' }))
    const editor = screen.getByRole('dialog', { name: 'Inbox splits' })

    await user.click(within(editor).getByRole('button', { name: 'Move Newsletters up' }))
    expect(get(prefs).splitTabs).toBe('newsletters,important')

    await user.click(within(editor).getByRole('button', { name: 'Add Clients' }))
    expect(get(prefs).splitTabs).toBe('newsletters,important,view:4')
    expect(screen.getAllByRole('tab').map((tab) => tab.textContent?.trim().split(/\s+/)[0])).toEqual([
      'Newsletters',
      'Important',
      'Clients',
      'Other',
    ])

    await user.click(within(editor).getByRole('button', { name: 'Remove Important' }))
    expect(get(prefs).splitTabs).toBe('newsletters,view:4')
    expect(api.setSetting).toHaveBeenLastCalledWith('split_inbox_tabs', 'newsletters,view:4')
  })
})
