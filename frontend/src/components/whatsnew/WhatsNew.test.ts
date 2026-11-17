import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

import WhatsNew from './WhatsNew.svelte'
import { whatsNewRelease } from '../../stores/whatsnew'
import { space } from '../../stores/space'
import { settingsRequest } from '../../stores/settingsnav'
import { historyOpen } from '../../stores/undohistory'
import { calendarView } from '../../stores/calendar'
import { releases } from '../../lib/whatsnew'
import { prefs } from '../../stores/prefs'

beforeEach(() => {
  prefs.update((p) => ({ ...p, reduceMotion: true }))
  whatsNewRelease.set(releases[0])
  space.set('mail')
  settingsRequest.set(null)
  historyOpen.set(false)
  calendarView.set('week')
})

describe("what's new", () => {
  it('lists every addition of the release with a Show me button', () => {
    render(WhatsNew)
    expect(screen.getByRole('heading', { name: "What's new" })).toBeInTheDocument()
    expect(screen.getByText('Projects')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /^Show me/ })).toHaveLength(releases[0].entries.length)
  })

  it('opens a space and closes', async () => {
    const close = vi.fn()
    render(WhatsNew, { events: { close } })
    await userEvent.click(screen.getByRole('button', { name: 'Show me: Projects' }))
    expect(get(space)).toBe('projects')
    expect(close).toHaveBeenCalled()
  })

  it('opens the undo history, a settings row and the year view', async () => {
    render(WhatsNew)
    await userEvent.click(screen.getByRole('button', { name: 'Show me: Undo history' }))
    expect(get(historyOpen)).toBe(true)
    await userEvent.click(screen.getByRole('button', { name: 'Show me: Hide and reorder spaces' }))
    expect(get(settingsRequest)).toBe('layout#spaces')
    await userEvent.click(screen.getByRole('button', { name: 'Show me: Year planner' }))
    expect(get(calendarView)).toBe('year')
    expect(get(space)).toBe('calendar')
  })

  it('says so when the version has no notes', () => {
    whatsNewRelease.set(null)
    render(WhatsNew)
    expect(screen.getByText('Nothing new to show for this version.')).toBeInTheDocument()
  })
})
