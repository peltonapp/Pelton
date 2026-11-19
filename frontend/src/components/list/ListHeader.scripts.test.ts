import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'

const run = vi.hoisted(() => vi.fn())

vi.mock('../../stores/scripts', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/scripts')>()),
  runPlaced: run,
}))

import ListHeader from './ListHeader.svelte'
import { placedScripts } from '../../stores/scripts'
import { selectedIds } from '../../stores/listselect'

beforeEach(() => {
  run.mockReset().mockResolvedValue(null)
  placedScripts.set({})
  selectedIds.set(new Set())
})

describe('ListHeader scripts', () => {
  it('renders a button for each script placed on the list toolbar', async () => {
    placedScripts.set({
      'toolbar.list': [{ scriptId: 6, name: 'Triage', description: '', options: { icon: 'funnel', label: 'Triage selected' } }],
    })
    selectedIds.set(new Set([11, 12]))
    render(ListHeader, { title: 'Inbox' })
    await userEvent.setup().click(screen.getByRole('button', { name: 'Triage selected' }))
    expect(run).toHaveBeenCalledWith(6, 'toolbar.list', [{ id: '11' }, { id: '12' }])
  })

  it('renders none when nothing is placed', () => {
    render(ListHeader, { title: 'Inbox' })
    expect(screen.queryByRole('button', { name: 'Triage selected' })).toBeNull()
  })
})
