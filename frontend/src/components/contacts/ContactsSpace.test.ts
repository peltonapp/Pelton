import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { AddressBook, Contact } from '../../lib/types'

const api = vi.hoisted(() => ({
  listAddressBooks: vi.fn(),
  listContacts: vi.fn(),
  syncContacts: vi.fn(),
  saveContact: vi.fn(),
  deleteContact: vi.fn(),
  search: vi.fn(),
  setSetting: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  listAddressBooks: api.listAddressBooks,
  listContacts: api.listContacts,
  syncContacts: api.syncContacts,
  saveContact: api.saveContact,
  deleteContact: api.deleteContact,
  search: api.search,
  setSetting: api.setSetting,
}))

import ContactsSpace from './ContactsSpace.svelte'
import { contactBookFilter, contactSeed } from '../../stores/contacts'
import { contactOrg, contactQuery, contactSort, selectedContactId } from '../../stores/contactsview'
import { confirmRequest } from '../../stores/confirm'
import { prefs } from '../../stores/prefs'

// jsdom has no Web Animations; a transition finishes as soon as it starts.
Element.prototype.animate = function animate() {
  const animation = { cancel(): void {}, finish(): void {}, pause(): void {}, play(): void {}, currentTime: 0 } as Animation
  let finished: Animation['onfinish'] = null
  Object.defineProperty(animation, 'onfinish', {
    get: () => finished,
    set: (fn: Animation['onfinish']) => {
      finished = fn
      setTimeout(() => fn?.call(animation, new Event('finish') as AnimationPlaybackEvent), 0)
    },
  })
  return animation
}

vi.stubGlobal(
  'ResizeObserver',
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
)

function book(id: number, patch: Partial<AddressBook> = {}): AddressBook {
  return {
    id,
    accountId: 0,
    name: `Book ${id}`,
    url: 'https://dav.example.com',
    collectionPath: '/c',
    username: 'me',
    readOnly: false,
    lastSync: '2026-01-01T00:00:00Z',
    lastError: '',
    contactCount: 3,
    hasPassword: true,
    ...patch,
  }
}

function contact(id: number, fullName: string, patch: Partial<Contact> = {}): Contact {
  return {
    id,
    bookId: 1,
    bookName: 'Book 1',
    uid: `uid-${id}`,
    fullName,
    organization: '',
    title: '',
    note: '',
    emails: [{ value: `${fullName.split(' ')[0].toLowerCase()}@example.com`, label: 'work' }],
    phones: [],
    readOnly: false,
    updated: '',
    extra: [],
    ...patch,
  }
}

const people = [
  contact(1, 'Ada Lovelace', { organization: 'Analytical Engines', phones: [{ value: '555 0100', label: 'home' }], note: 'Met at the fair' }),
  contact(2, 'Alan Turing', { organization: 'Bletchley' }),
  contact(3, 'Grace Hopper'),
]

beforeEach(() => {
  api.listAddressBooks.mockReset().mockResolvedValue([book(1)])
  api.listContacts.mockReset().mockResolvedValue(people)
  api.syncContacts.mockReset().mockResolvedValue(undefined)
  api.saveContact.mockReset().mockResolvedValue({ conflict: false })
  api.deleteContact.mockReset().mockResolvedValue({ conflict: false })
  api.search.mockReset().mockResolvedValue({ messages: [], total: 0 })
  api.setSetting.mockReset().mockResolvedValue(undefined)
  contactBookFilter.set(0)
  contactSeed.set(null)
  contactQuery.set('')
  contactSort.set('name')
  contactOrg.set(null)
  selectedContactId.set(null)
  confirmRequest.set(null)
  prefs.update((p) => ({ ...p, design: 'new', contactsView: 'panes' }))
})

describe('ContactsSpace list', () => {
  it('lists contacts under letter headers', async () => {
    render(ContactsSpace)
    await screen.findByText('Ada Lovelace')
    const headers = screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent)
    expect(headers).toContain('A')
    expect(headers).toContain('G')
    expect(screen.getByRole('button', { name: /Alan Turing/ })).toBeInTheDocument()
  })

  it('narrows the list as the search is typed', async () => {
    const user = userEvent.setup()
    render(ContactsSpace)
    await screen.findByText('Ada Lovelace')
    await user.type(screen.getByRole('searchbox', { name: 'Search contacts' }), 'hopper')
    expect(screen.queryByText('Ada Lovelace')).toBeNull()
    expect(screen.getByText('Grace Hopper')).toBeInTheDocument()
    await user.clear(screen.getByRole('searchbox', { name: 'Search contacts' }))
    await user.type(screen.getByRole('searchbox', { name: 'Search contacts' }), 'nobody')
    expect(screen.getByText('No contact matches that.')).toBeInTheDocument()
  })

  it('narrows to an organization picked in the book pane', async () => {
    const user = userEvent.setup()
    render(ContactsSpace)
    await screen.findByText('Ada Lovelace')
    const nav = screen.getByRole('navigation', { name: 'Address books' })
    await user.click(within(nav).getByRole('button', { name: /Bletchley/ }))
    expect(screen.queryByText('Ada Lovelace')).toBeNull()
    expect(screen.getByText('Alan Turing')).toBeInTheDocument()
  })
})

describe('ContactsSpace detail', () => {
  it('shows the contact with its numbers, note and actions', async () => {
    const user = userEvent.setup()
    render(ContactsSpace)
    await user.click(await screen.findByRole('button', { name: /Ada Lovelace/ }))
    const detail = await screen.findByRole('article', { name: 'Ada Lovelace' })
    expect(within(detail).getByText('Analytical Engines')).toBeInTheDocument()
    expect(within(detail).getByText('555 0100')).toBeInTheDocument()
    expect(within(detail).getByText('Met at the fair')).toBeInTheDocument()
    expect(within(detail).getByRole('button', { name: 'Write mail' })).toBeInTheDocument()
    expect(within(detail).getByRole('button', { name: 'Edit' })).toBeInTheDocument()
  })

  it('lists recent mail from the search index', async () => {
    api.search.mockResolvedValue({
      messages: [{ id: 9, accountId: 1, folderId: 2, subject: 'Notes on the engine', date: '2026-02-01T10:00:00Z' }],
      total: 1,
    })
    const user = userEvent.setup()
    render(ContactsSpace)
    await user.click(await screen.findByRole('button', { name: /Ada Lovelace/ }))
    await screen.findByText('Notes on the engine')
    expect(api.search).toHaveBeenCalledWith(expect.objectContaining({ from: 'ada@example.com' }))
  })

  it('writes to the contact through the compose event', async () => {
    const user = userEvent.setup()
    const handler = vi.fn()
    render(ContactsSpace, { events: { compose: handler } } as never)
    await user.click(await screen.findByRole('button', { name: /Ada Lovelace/ }))
    await user.click(await screen.findByRole('button', { name: 'Write mail' }))
    expect(handler).toHaveBeenCalledTimes(1)
    expect((handler.mock.calls[0][0] as CustomEvent).detail).toBe('ada@example.com')
  })

  it('asks before deleting and then deletes', async () => {
    const user = userEvent.setup()
    render(ContactsSpace)
    await user.click(await screen.findByRole('button', { name: /Ada Lovelace/ }))
    const detail = await screen.findByRole('article', { name: 'Ada Lovelace' })
    await user.click(within(detail).getByRole('button', { name: 'Delete' }))
    expect(api.deleteContact).not.toHaveBeenCalled()
    const alert = within(detail).getByRole('alert')
    await user.click(within(alert).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(api.deleteContact).toHaveBeenCalledWith(1, false))
  })

  it('hides edit and delete for a read-only contact', async () => {
    api.listContacts.mockResolvedValue([contact(1, 'Ada Lovelace', { readOnly: true })])
    const user = userEvent.setup()
    render(ContactsSpace)
    await user.click(await screen.findByRole('button', { name: /Ada Lovelace/ }))
    const detail = await screen.findByRole('article', { name: 'Ada Lovelace' })
    expect(within(detail).queryByRole('button', { name: 'Edit' })).toBeNull()
    expect(within(detail).queryByRole('button', { name: 'Delete' })).toBeNull()
  })
})

describe('ContactsSpace editor', () => {
  it('opens a prefilled editor for a contact handed over from mail', async () => {
    contactSeed.set({ name: 'Linus T', email: 'linus@example.com' })
    render(ContactsSpace)
    const form = await screen.findByRole('form', { name: 'New contact' })
    expect(within(form).getByDisplayValue('Linus T')).toBeInTheDocument()
    expect(within(form).getByDisplayValue('linus@example.com')).toBeInTheDocument()
    expect(get(contactSeed)).toBeNull()
  })

  it('saves an edit through the API and closes the editor', async () => {
    const user = userEvent.setup()
    render(ContactsSpace)
    await user.click(await screen.findByRole('button', { name: /Grace Hopper/ }))
    await user.click(await screen.findByRole('button', { name: 'Edit' }))
    const form = await screen.findByRole('form', { name: 'Edit contact' })
    const name = within(form).getByDisplayValue('Grace Hopper')
    await user.clear(name)
    await user.type(name, 'Grace B. Hopper')
    await user.click(within(form).getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(api.saveContact).toHaveBeenCalled())
    await waitFor(() => expect(screen.queryByRole('form', { name: 'Edit contact' })).toBeNull())
  })

  it('asks before an unsaved edit is thrown away by opening another contact', async () => {
    const user = userEvent.setup()
    render(ContactsSpace)
    await user.click(await screen.findByRole('button', { name: /Grace Hopper/ }))
    await user.click(await screen.findByRole('button', { name: 'Edit' }))
    const form = await screen.findByRole('form', { name: 'Edit contact' })
    await user.type(within(form).getByDisplayValue('Grace Hopper'), '!')
    await user.click(screen.getByRole('button', { name: /Alan Turing/ }))
    await waitFor(() => expect(get(confirmRequest)).not.toBeNull())
    expect(get(confirmRequest)?.title).toBe('Discard your changes?')
    get(confirmRequest)?.resolve(false)
    expect(get(selectedContactId)).toBe(3)
  })
})

describe('ContactsSpace view switch', () => {
  it('moves to the card grid and stores the choice', async () => {
    const user = userEvent.setup()
    render(ContactsSpace)
    await screen.findByText('Ada Lovelace')
    expect(screen.getByRole('button', { name: 'List and detail', pressed: true })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Card grid' }))
    expect(get(prefs).contactsView).toBe('grid')
    expect(api.setSetting).toHaveBeenCalledWith('contacts_view', 'grid')
    expect(screen.getByRole('button', { name: /Ada Lovelace/ })).toBeInTheDocument()
    expect(screen.queryByText('Pick a contact to see it.')).toBeNull()
  })

  it('opens a card in a drawer that can be closed', async () => {
    prefs.update((p) => ({ ...p, contactsView: 'grid' }))
    const user = userEvent.setup()
    render(ContactsSpace)
    await user.click(await screen.findByRole('button', { name: /Ada Lovelace/ }))
    const detail = await screen.findByRole('article', { name: 'Ada Lovelace' })
    await user.click(within(detail).getByRole('button', { name: 'Close' }))
    await waitFor(() => expect(screen.queryByRole('article', { name: 'Ada Lovelace' })).toBeNull())
  })
})

describe('ContactsSpace without a book', () => {
  it('offers to add one', async () => {
    api.listAddressBooks.mockResolvedValue([])
    api.listContacts.mockResolvedValue([])
    render(ContactsSpace)
    expect(await screen.findByRole('button', { name: 'Add an address book' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'New contact' })).toBeDisabled()
  })
})
