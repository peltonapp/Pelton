import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  listAddressBooks: vi.fn(),
  listAccounts: vi.fn(),
  setSetting: vi.fn(),
}))

vi.mock('../../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../../lib/api')>()),
  listAddressBooks: api.listAddressBooks,
  listAccounts: api.listAccounts,
  setSetting: api.setSetting,
}))

import Contacts from './Contacts.svelte'
import { prefs } from '../../../stores/prefs'

beforeEach(() => {
  api.listAddressBooks.mockReset().mockResolvedValue([])
  api.listAccounts.mockReset().mockResolvedValue([])
  api.setSetting.mockReset().mockResolvedValue(undefined)
  prefs.update((p) => ({ ...p, design: 'new', contactsView: 'panes' }))
})

describe('Contacts settings page', () => {
  it('offers the two views as cards and marks the current one', async () => {
    render(Contacts)
    const group = await screen.findByRole('radiogroup', { name: 'Contacts view' })
    expect(group).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: /List and detail/ })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByRole('radio', { name: /Card grid/ })).toHaveAttribute('aria-checked', 'false')
  })

  it('stores the view that is clicked', async () => {
    const user = userEvent.setup()
    render(Contacts)
    await user.click(await screen.findByRole('radio', { name: /Card grid/ }))
    expect(get(prefs).contactsView).toBe('grid')
    expect(api.setSetting).toHaveBeenCalledWith('contacts_view', 'grid')
  })
})
