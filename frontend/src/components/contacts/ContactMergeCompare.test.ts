import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { AddressBook, Contact, DuplicateGroup, MergeResult } from '../../lib/types'

const api = vi.hoisted(() => ({
  mergeContacts: vi.fn(),
  undoContactMerge: vi.fn(),
  dismissDuplicateContacts: vi.fn(),
  resetDismissedDuplicates: vi.fn(),
  findDuplicateContacts: vi.fn(),
  listAddressBooks: vi.fn(),
  listContacts: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

// dialogs open with transitions and jsdom has no web animations.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

import ContactMergeCompare from './ContactMergeCompare.svelte'
import ContactDuplicates from './ContactDuplicates.svelte'
import { duplicateGroups, dismissedPairs, lastMerge } from '../../stores/duplicates'
import { toasts } from '../../stores/toast'

function book(id: number, patch: Partial<AddressBook> = {}): AddressBook {
  return {
    id,
    accountId: 0,
    name: `Book ${id}`,
    url: 'https://dav.example.com',
    collectionPath: '/c',
    username: 'me',
    readOnly: false,
    lastSync: '',
    lastError: '',
    contactCount: 2,
    hasPassword: true,
    ...patch,
  }
}

function contact(id: number, patch: Partial<Contact> = {}): Contact {
  return {
    id,
    bookId: 1,
    bookName: 'Book 1',
    uid: `u${id}`,
    fullName: '',
    organization: '',
    title: '',
    note: '',
    emails: [],
    phones: [],
    matrixId: '',
    readOnly: false,
    updated: '',
    extra: [],
    ...patch,
  }
}

const ada = contact(1, { fullName: 'Ada Lovelace', note: 'first', emails: [{ value: 'ada@work.example', label: 'work' }] })
const augusta = contact(2, {
  fullName: 'Augusta Ada King',
  organization: 'Engine Co',
  note: 'second',
  emails: [{ value: 'ADA@work.example', label: '' }, { value: 'ada@home.example', label: 'home' }],
  phones: [{ value: '+31 6 1234 5678', label: '' }],
  extra: ['BDAY'],
})

const merged: MergeResult = { contact: { ...ada, id: 1 }, token: 'tok', kept: 0 }

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.mergeContacts.mockResolvedValue(merged)
  api.listAddressBooks.mockResolvedValue([book(1)])
  api.listContacts.mockResolvedValue([])
  api.findDuplicateContacts.mockResolvedValue({ groups: [], dismissed: 0 })
  api.undoContactMerge.mockResolvedValue([ada, augusta])
  api.dismissDuplicateContacts.mockResolvedValue(undefined)
  api.resetDismissedDuplicates.mockResolvedValue(1)
  lastMerge.set(null)
  duplicateGroups.set([])
  dismissedPairs.set(0)
  toasts.set([])
})

function mount(group: Contact[], books: AddressBook[] = [book(1)]) {
  const events = { close: vi.fn(), merged: vi.fn() }
  render(ContactMergeCompare, { props: { group, books }, events })
  return events
}

describe('ContactMergeCompare', () => {
  it('shows a column per card and the merged result', () => {
    mount([ada, augusta])
    expect(screen.getAllByRole('radio', { name: 'Keep this card' })).toHaveLength(2)
    const result = screen.getByRole('region', { name: 'Merged contact' })
    expect(result).toHaveTextContent('ADA@work.example, ada@home.example')
    expect(result).toHaveTextContent('+31 6 1234 5678')
  })

  it('keeps the card with the most details by default', () => {
    mount([ada, augusta])
    const keep = screen.getAllByRole('radio', { name: 'Keep this card' })
    expect(keep[0]).not.toBeChecked()
    expect(keep[1]).toBeChecked()
  })

  it('offers a pick only for fields the cards disagree about', () => {
    mount([ada, augusta])
    expect(within(screen.getByRole('radiogroup', { name: 'Pick the name' })).getAllByRole('radio')).toHaveLength(2)
    expect(within(screen.getByRole('radiogroup', { name: 'Pick the organisation' })).queryAllByRole('radio')).toHaveLength(0)
  })

  it('updates the result as a field is picked', async () => {
    mount([ada, augusta])
    const result = screen.getByRole('region', { name: 'Merged contact' })
    expect(result).toHaveTextContent('Augusta Ada King')
    await userEvent.click(within(screen.getByRole('radiogroup', { name: 'Pick the name' })).getByRole('radio', { name: 'Ada Lovelace' }))
    expect(result).toHaveTextContent('Ada Lovelace')
    expect(result).not.toHaveTextContent('Augusta')
  })

  it('can keep both notes', async () => {
    mount([ada, augusta])
    await userEvent.click(screen.getByRole('radio', { name: 'Keep both' }))
    expect(screen.getByRole('region', { name: 'Merged contact' })).toHaveTextContent(/first\s+second/)
  })

  it('sends the survivor, the others and the picks on merge', async () => {
    const events = mount([ada, augusta])
    await userEvent.click(within(screen.getByRole('radiogroup', { name: 'Pick the name' })).getByRole('radio', { name: 'Ada Lovelace' }))
    await userEvent.click(screen.getByRole('radio', { name: 'Keep both' }))
    await userEvent.click(screen.getByRole('button', { name: 'Merge 2 contacts' }))
    await waitFor(() => expect(api.mergeContacts).toHaveBeenCalledTimes(1))
    expect(api.mergeContacts.mock.calls[0][0]).toEqual({
      survivorId: 2,
      mergeIds: [1],
      targetBookId: 0,
      choices: { name: 1, organization: 2, title: 2, note: -1 },
    })
    await waitFor(() => expect(events.merged).toHaveBeenCalled())
  })

  it('moves the picks along when another card is kept', async () => {
    mount([ada, augusta])
    await userEvent.click(screen.getAllByRole('radio', { name: 'Keep this card' })[0])
    await userEvent.click(screen.getByRole('button', { name: 'Merge 2 contacts' }))
    await waitFor(() => expect(api.mergeContacts).toHaveBeenCalled())
    const req = api.mergeContacts.mock.calls[0][0]
    expect(req.survivorId).toBe(1)
    expect(req.mergeIds).toEqual([2])
    expect(req.choices.name).toBe(1)
  })

  it('asks which address book the merged contact goes in when the cards are in several', async () => {
    const other = contact(3, { bookId: 2, bookName: 'Book 2', fullName: 'Ada L', emails: [{ value: 'ada@work.example', label: '' }] })
    mount([ada, other], [book(1), book(2)])
    const merge = screen.getByRole('button', { name: 'Merge 2 contacts' })
    expect(merge).toBeDisabled()
    expect(screen.getByText(/different address books/)).toBeInTheDocument()

    await userEvent.click(screen.getByRole('combobox', { name: 'Address book for the merged contact' }))
    await userEvent.click(await screen.findByRole('option', { name: 'Book 2' }))
    expect(merge).toBeEnabled()
    await userEvent.click(merge)
    await waitFor(() => expect(api.mergeContacts).toHaveBeenCalled())
    expect(api.mergeContacts.mock.calls[0][0].targetBookId).toBe(2)
  })

  it('refuses to merge into or out of a read-only book', () => {
    mount([ada, augusta], [book(1, { readOnly: true, name: 'Shared' })])
    expect(screen.getByRole('alert')).toHaveTextContent('Shared is read-only')
    expect(screen.getByRole('button', { name: 'Merge 2 contacts' })).toBeDisabled()
  })

  it('reports a failed merge and stays open', async () => {
    api.mergeContacts.mockRejectedValue(new Error('pelton: a contact changed on its server since it was last synced'))
    const events = mount([ada, augusta])
    await userEvent.click(screen.getByRole('button', { name: 'Merge 2 contacts' }))
    await waitFor(() => expect(get(toasts).some((x) => x.kind === 'error')).toBe(true))
    expect(events.merged).not.toHaveBeenCalled()
  })

  it('closes on Cancel without writing', async () => {
    const events = mount([ada, augusta])
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(events.close).toHaveBeenCalled()
    expect(api.mergeContacts).not.toHaveBeenCalled()
  })
})

describe('ContactDuplicates', () => {
  const group: DuplicateGroup = {
    contacts: [ada, augusta],
    score: 100,
    reasons: [{ kind: 'email', value: 'ada@work.example', overlap: [] }],
  }

  it('lists a group with its reason and score', () => {
    duplicateGroups.set([group])
    render(ContactDuplicates)
    expect(screen.getByText('Very likely')).toBeInTheDocument()
    expect(screen.getByText('Score 100')).toBeInTheDocument()
    expect(screen.getByText('Same email address: ada@work.example')).toBeInTheDocument()
    expect(screen.getByText('Ada Lovelace')).toBeInTheDocument()
  })

  it('says so when there are none', () => {
    render(ContactDuplicates)
    expect(screen.getByRole('heading', { name: 'No duplicates found' })).toBeInTheDocument()
  })

  it('dismisses a group for good', async () => {
    duplicateGroups.set([group])
    render(ContactDuplicates)
    await userEvent.click(screen.getByRole('button', { name: 'Not duplicates' }))
    await waitFor(() => expect(api.dismissDuplicateContacts).toHaveBeenCalledWith([1, 2]))
    await waitFor(() => expect(screen.getByRole('heading', { name: 'No duplicates found' })).toBeInTheDocument())
  })

  it('opens the compare and, after a merge, offers to undo it', async () => {
    duplicateGroups.set([group])
    render(ContactDuplicates)
    await userEvent.click(screen.getByRole('button', { name: 'Compare and merge' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Merge 2 contacts' }))
    await waitFor(() => expect(api.mergeContacts).toHaveBeenCalled())
    const statuses = await screen.findAllByRole('status')
    const status = statuses.find((s) => within(s).queryByRole('button', { name: 'Undo merge' }))!
    await userEvent.click(within(status).getByRole('button', { name: 'Undo merge' }))
    await waitFor(() => expect(api.undoContactMerge).toHaveBeenCalledWith('tok'))
    await waitFor(() => expect(get(lastMerge)).toBeNull())
  })

  it('shows the dismissed groups again on request', async () => {
    dismissedPairs.set(2)
    render(ContactDuplicates)
    await userEvent.click(screen.getByRole('button', { name: 'Show dismissed groups again' }))
    await waitFor(() => expect(api.resetDismissedDuplicates).toHaveBeenCalled())
  })
})
