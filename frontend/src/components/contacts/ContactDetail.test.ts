import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { Contact } from '../../lib/types'

const api = vi.hoisted(() => ({
  search: vi.fn(),
  chatOpenDirect: vi.fn(),
  chatRooms: vi.fn(),
  listTasks: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

import ContactDetail from './ContactDetail.svelte'
import { prefs } from '../../stores/prefs'
import { chatStatus, signedOutStatus } from '../../stores/chat'
import { toasts } from '../../stores/toast'
import { get } from 'svelte/store'

// the detail flies in, and jsdom has no web animations.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

function contact(patch: Partial<Contact> = {}): Contact {
  return {
    id: 1,
    bookId: 1,
    bookName: 'Book',
    uid: 'u1',
    fullName: 'Ada Lovelace',
    organization: '',
    title: '',
    note: '',
    emails: [],
    phones: [],
    matrixId: '@ada:example.org',
    readOnly: false,
    updated: '',
    extra: [],
    ...patch,
  }
}

function signIn(): void {
  chatStatus.set({ ...signedOutStatus, state: 'online' })
}

beforeEach(() => {
  api.search.mockReset().mockResolvedValue({ messages: [], total: 0 })
  api.chatOpenDirect.mockReset().mockResolvedValue('!room:example.org')
  api.chatRooms.mockReset().mockResolvedValue([])
  api.listTasks.mockReset().mockResolvedValue([])
  prefs.update((p) => ({ ...p, chatEnabled: true }))
  chatStatus.set(signedOutStatus)
  toasts.set([])
})

describe('ContactDetail chat button', () => {
  it('shows the Matrix ID and a Chat button when chat is available', async () => {
    signIn()
    render(ContactDetail, { contact: contact() })
    expect(await screen.findByText('@ada:example.org')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Chat' })).toBeInTheDocument()
  })

  it('hides the button when nobody is signed in to chat', async () => {
    render(ContactDetail, { contact: contact() })
    expect(await screen.findByText('@ada:example.org')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Chat' })).toBeNull()
  })

  it('hides the button when chat is turned off', async () => {
    signIn()
    prefs.update((p) => ({ ...p, chatEnabled: false }))
    render(ContactDetail, { contact: contact() })
    await screen.findByText('@ada:example.org')
    expect(screen.queryByRole('button', { name: 'Chat' })).toBeNull()
  })

  it('hides the button without a Matrix ID', async () => {
    signIn()
    render(ContactDetail, { contact: contact({ matrixId: '' }) })
    await screen.findByRole('heading', { name: 'Ada Lovelace' })
    expect(screen.queryByRole('button', { name: 'Chat' })).toBeNull()
  })

  it('toasts the error when the chat cannot be opened', async () => {
    signIn()
    api.chatOpenDirect.mockRejectedValue(new Error('chat: no such user'))
    const user = userEvent.setup()
    render(ContactDetail, { contact: contact() })
    await user.click(await screen.findByRole('button', { name: 'Chat' }))
    await waitFor(() => expect(get(toasts).some((x) => x.message === 'No such user')).toBe(true))
    expect(api.chatOpenDirect).toHaveBeenCalledWith('@ada:example.org')
  })
})
