import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'

const api = vi.hoisted(() => ({
  isDevMode: vi.fn(async () => false),
  isNightly: vi.fn(async () => false),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import StatusBar from './StatusBar.svelte'
import { emptySyncCounts, syncCounts, syncing, syncPhase } from '../../stores/outbox'
import { prefs } from '../../stores/prefs'

describe('StatusBar folder check line', () => {
  afterEach(() => {
    syncing.set(false)
    syncPhase.set('')
    syncCounts.set(emptySyncCounts)
  })

  it('shows the background folder check calmly, with no spinner or bar', () => {
    prefs.update((p) => ({ ...p, syncProgressBar: true }))
    syncing.set(false)
    syncPhase.set('verify')
    syncCounts.set({ ...emptySyncCounts, foldersDone: 1, foldersTotal: 3 })
    const { container } = render(StatusBar)
    expect(screen.getByText('Checking folders (1/3)')).toBeInTheDocument()
    expect(screen.queryByRole('progressbar')).toBeNull()
    expect(container.querySelector('.spin')).toBeNull()
  })
})
