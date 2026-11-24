import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import AccountTree from './AccountTree.svelte'
import { prefs } from '../../stores/prefs'
import { vaultDialog } from '../../stores/vaultdialog'
import { expectAccessible } from '../../test/a11y'
import type { Account, Folder } from '../../lib/types'

const account = { id: 1, email: 'me@example.com', displayName: '', localLabel: '', useLocalLabel: false, local: false } as unknown as Account

function folder(id: number, name: string, vault: Partial<Folder> = {}): Folder {
  return {
    id, accountId: 1, name, parentId: null, role: id === 1 ? 'inbox' : 'normal', unreadCount: id === 1 ? 2 : 0, totalCount: 2,
    delimiter: '/', pinned: false, syncExcluded: false, vaultId: 0, vaultLocked: false, vaultHidden: false, ...vault,
  } as unknown as Folder
}

function tree(locked: boolean, hidden = false): Folder[] {
  return [folder(1, 'INBOX'), folder(2, 'Lawyer', { vaultId: 7, vaultLocked: locked, vaultHidden: hidden })]
}

beforeEach(() => vaultDialog.set(null))
afterEach(() => prefs.update((p) => ({ ...p, design: 'new' })))

describe('a locked vault folder in the sidebar', () => {
  it('is a locked entry without counts in the new design, and opens the unlock dialog', async () => {
    prefs.update((p) => ({ ...p, design: 'new' }))
    render(AccountTree, { props: { account, folders: tree(true) } })
    const row = screen.getByTitle('In a locked vault. Click to unlock.')
    expect(row).toHaveTextContent('Lawyer')
    expect(screen.queryByText('Unlock vault')).not.toBeInTheDocument()
    expectAccessible()
    await userEvent.click(screen.getByText('Lawyer'))
    expect(get(vaultDialog)).toEqual({ kind: 'unlock', vaultId: 7 })
  })

  it('is hidden in the classic design, with one unlock entry instead', async () => {
    prefs.update((p) => ({ ...p, design: 'classic' }))
    render(AccountTree, { props: { account, folders: tree(true) } })
    expect(screen.queryByText('Lawyer')).not.toBeInTheDocument()
    expect(screen.getByText('INBOX')).toBeInTheDocument()
    await userEvent.click(screen.getByText('Unlock vault'))
    expect(get(vaultDialog)).toEqual({ kind: 'unlock', vaultId: 7 })
  })

  it('is hidden in the new design when the vault asks for it', () => {
    prefs.update((p) => ({ ...p, design: 'new' }))
    render(AccountTree, { props: { account, folders: tree(true, true) } })
    expect(screen.queryByText('Lawyer')).not.toBeInTheDocument()
    expect(screen.getByText('Unlock vault')).toBeInTheDocument()
  })

  it('shows normally once unlocked, with a lock now entry', () => {
    prefs.update((p) => ({ ...p, design: 'new' }))
    render(AccountTree, { props: { account, folders: tree(false) } })
    expect(screen.getByText('Lawyer')).toBeInTheDocument()
    expect(screen.getByText('Lock vault now')).toBeInTheDocument()
  })
})
