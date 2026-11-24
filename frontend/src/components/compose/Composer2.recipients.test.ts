import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createEvent, fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  composeProtectionStatus: vi.fn(),
  searchAddresses: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../stores/signatures', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/signatures')>()),
  getAccountSignatures: vi.fn().mockResolvedValue({ headerId: 0, footerId: 0 }),
}))

import Composer2 from './Composer2.svelte'
import { composeSessions, getSession, openCompose, openReply, updateCompose, type ComposeSession } from '../../stores/compose'
import { extraPaletteCommands } from '../../lib/palettesources'
import type { CommandStep } from '../../lib/commands'
import { prefs } from '../../stores/prefs'
import { t } from '../../lib/i18n'
import type { MessageDetail } from '../../lib/types'

let unsubscribe: (() => void) | null = null

function mount(id: number): void {
  updateCompose(id, { signaturesApplied: true })
  const session = getSession(id) as ComposeSession
  const { rerender } = render(Composer2, { props: { session, layout: 'window' } })
  unsubscribe = composeSessions.subscribe((list) => {
    const next = list.find((s) => s.id === id)
    if (next) {
      void rerender({ session: next, layout: 'window' })
    }
  })
}

// an introduction: Ada wrote to the user and Bob, introducing them.
function intro(): MessageDetail {
  return {
    id: 42,
    accountId: 1,
    folderId: 20,
    accountEmail: 'me@example.com',
    fromName: 'Ada',
    fromAddress: 'ada@example.com',
    toAddresses: 'me@example.com',
    ccAddresses: 'Bob <bob@example.com>',
    subject: 'Meet Bob',
    date: '2026-10-01',
    bodyQuote: 'You two should talk.',
    messageId: '<n1@x>',
    references: [],
    inReplyTo: [],
  } as unknown as MessageDetail
}

function chip(value: string): HTMLElement {
  return screen.getByRole('button', { name: new RegExp(`^${value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')} in`) })
}

beforeEach(() => {
  document.documentElement.setAttribute('data-reduce-motion', '')
  api.composeProtectionStatus.mockReset().mockResolvedValue({ canSign: false, canEncrypt: false, suggested: 'none', recipients: [] })
  api.searchAddresses.mockReset().mockResolvedValue([])
  prefs.update((p) => ({ ...p, reduceMotion: true, composeVimMode: true, composeChips: true, composeAutocomplete: false }))
})

afterEach(() => {
  document.documentElement.removeAttribute('data-reduce-motion')
  unsubscribe?.()
  unsubscribe = null
  composeSessions.set([])
})

describe('quick recipient moves', () => {
  it('moves the introducer to Bcc and thanks them', async () => {
    const id = openReply(intro(), 'plaintext', true)
    mount(id)
    expect(getSession(id)?.to).toBe('Ada <ada@example.com>')

    screen.getByRole('textbox', { name: 'Subject' }).focus()
    await userEvent.keyboard('{Control>}{Shift>}b{/Shift}{/Control}')

    const s = getSession(id)
    expect(s?.to).toBe('Bob <bob@example.com>')
    expect(s?.cc).toBe('')
    expect(s?.bcc).toBe('Ada <ada@example.com>')
    expect(s?.body.startsWith('Thanks for the intro, moving you to Bcc.')).toBe(true)
  })

  it('switches a reply to reply all and back', async () => {
    const id = openReply(intro(), 'plaintext', false)
    mount(id)

    const entry = extraPaletteCommands(get(t)).find((c) => c.id === 'compose:compose-toggle-reply-all')
    entry?.run()
    await waitFor(() => expect(getSession(id)?.origin?.kind).toBe('replyAll'))
    expect(getSession(id)?.cc).toBe('Bob <bob@example.com>')

    entry?.run()
    await waitFor(() => expect(getSession(id)?.origin?.kind).toBe('reply'))
  })

  it('adds the original sender to Cc', async () => {
    const id = openReply(intro(), 'plaintext', false)
    updateCompose(id, { to: 'bob@example.com' })
    mount(id)

    extraPaletteCommands(get(t)).find((c) => c.id === 'compose:compose-cc-sender')?.run()

    await waitFor(() => expect(getSession(id)?.cc).toBe('Ada <ada@example.com>'))
    expect(getSession(id)?.showCc).toBe(true)
  })

  it('moves a focused chip down and up with its keys, keeping focus on it', async () => {
    const id = openCompose(1, 'plaintext')
    updateCompose(id, { to: 'ann@example.com, bob@example.com' })
    mount(id)

    chip('ann@example.com').focus()
    await userEvent.keyboard('{Alt>}{ArrowDown}{/Alt}')
    expect(getSession(id)).toMatchObject({ to: 'bob@example.com', cc: 'ann@example.com', showCc: true })
    await waitFor(() => expect(document.activeElement?.getAttribute('data-chip-field')).toBe('cc'))

    await userEvent.keyboard('{Alt>}{ArrowDown}{/Alt}')
    expect(getSession(id)).toMatchObject({ cc: '', bcc: 'ann@example.com', showBcc: true })

    await waitFor(() => expect(document.activeElement?.getAttribute('data-chip-field')).toBe('bcc'))
    await userEvent.keyboard('{Alt>}{ArrowUp}{/Alt}')
    expect(getSession(id)).toMatchObject({ cc: 'ann@example.com', bcc: '' })
  })

  it('moves a recipient picked in the command palette', async () => {
    const id = openCompose(1, 'plaintext')
    updateCompose(id, { to: 'ann@example.com', cc: 'bob@example.com', showCc: true })
    mount(id)

    const step = extraPaletteCommands(get(t))
      .find((c) => c.id === 'compose:compose-recipient-down')
      ?.run() as CommandStep
    expect(step.items.map((i) => `${i.label} ${i.hint}`)).toEqual(['ann@example.com To → Cc', 'bob@example.com Cc → Bcc'])
    step.items[1].run()

    await waitFor(() => expect(getSession(id)).toMatchObject({ cc: '', bcc: 'bob@example.com' }))
  })

  it('moves a chip dragged onto another field', async () => {
    const id = openCompose(1, 'plaintext')
    updateCompose(id, { to: 'ann@example.com', showCc: true })
    mount(id)

    const store = new Map<string, string>()
    const dataTransfer = {
      setData: (type: string, value: string) => store.set(type, value),
      getData: (type: string) => store.get(type) ?? '',
      get types() {
        return [...store.keys()]
      },
      effectAllowed: 'all',
    }
    await fireEvent(chip('ann@example.com'), Object.assign(createEvent.dragStart(chip('ann@example.com')), { dataTransfer }))
    const cc = screen.getByRole('group', { name: 'Cc' })
    await fireEvent(cc, Object.assign(createEvent.dragOver(cc), { dataTransfer }))
    await fireEvent(cc, Object.assign(createEvent.drop(cc), { dataTransfer }))

    expect(getSession(id)).toMatchObject({ to: '', cc: 'ann@example.com' })
  })

  it('leaves Alt and an arrow alone in the subject', async () => {
    const id = openCompose(1, 'plaintext')
    updateCompose(id, { to: 'ann@example.com' })
    mount(id)
    screen.getByRole('textbox', { name: 'Subject' }).focus()
    await userEvent.keyboard('{Alt>}{ArrowDown}{/Alt}')
    expect(getSession(id)?.to).toBe('ann@example.com')
  })
})
