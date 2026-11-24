import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import HistoryButton from './HistoryButton.svelte'
import { expectAccessible } from '../../test/a11y'
import { clearHistory, history, historyOpen, recordHistory, stateKey, type HistoryInput } from '../../stores/undohistory'

function input(patch: Partial<HistoryInput> = {}): HistoryInput {
  return {
    kind: 'archive',
    label: 'Archived',
    detail: 'Quarterly report',
    count: 1,
    touches: [stateKey('flag', 1)],
    undo: vi.fn().mockResolvedValue(undefined),
    redo: vi.fn().mockResolvedValue(undefined),
    ...patch,
  }
}

beforeEach(() => {
  clearHistory()
  historyOpen.set(false)
})

describe('HistoryButton', () => {
  it('says so when there is nothing to undo', async () => {
    render(HistoryButton)
    await userEvent.click(screen.getByRole('button', { name: 'Show recent actions' }))
    const dialog = screen.getByRole('dialog', { name: 'Recent actions' })
    expect(within(dialog).getByText(/Nothing to undo yet/)).toBeInTheDocument()
  })

  it('lists entries newest first and undoes one from its row', async () => {
    const first = input({ label: 'Flagged', detail: 'Invoice', touches: [stateKey('flag', 2)] })
    recordHistory(first)
    recordHistory(input())
    render(HistoryButton)
    await userEvent.click(screen.getByRole('button', { name: 'Show recent actions' }))

    const items = within(screen.getByRole('list', { name: 'Recent actions' })).getAllByRole('listitem')
    expect(items[0]).toHaveTextContent('Archived')
    expect(items[1]).toHaveTextContent('Flagged')

    await userEvent.click(screen.getByRole('button', { name: 'Undo: Flagged' }))
    expect(first.undo).toHaveBeenCalledTimes(1)
    await vi.waitFor(() => expect(screen.getByRole('button', { name: 'Redo: Flagged' })).toBeEnabled())
  })

  it('shows why an earlier entry cannot be undone yet', async () => {
    recordHistory(input({ label: 'Flagged' }))
    recordHistory(input({ label: 'Unflagged' }))
    render(HistoryButton)
    await userEvent.click(screen.getByRole('button', { name: 'Show recent actions' }))

    expect(screen.getByRole('button', { name: 'Undo: Flagged' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Undo: Unflagged' })).toBeEnabled()
    expect(within(screen.getByRole('dialog', { name: 'Recent actions' })).getByText(/A later action changed the same messages/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Undo: Flagged' })).toHaveAccessibleDescription(/A later action changed the same messages/)
    expect(get(history)[1].reason).toBe('later')
  })

  it('moves focus into the panel and hands it back on Escape', async () => {
    recordHistory(input({ label: 'Flagged' }))
    render(HistoryButton)
    const trigger = screen.getByRole('button', { name: 'Show recent actions' })
    await userEvent.click(trigger)
    const panel = screen.getByRole('dialog', { name: 'Recent actions' })
    expect(panel.contains(document.activeElement)).toBe(true)
    expectAccessible()
    await userEvent.keyboard('{Escape}')
    expect(document.activeElement).toBe(trigger)
  })

  it('closes from its close button', async () => {
    render(HistoryButton)
    await userEvent.click(screen.getByRole('button', { name: 'Show recent actions' }))
    await userEvent.click(screen.getByRole('button', { name: 'Close recent actions' }))
    expect(get(historyOpen)).toBe(false)
  })
})
