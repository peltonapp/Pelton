import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  sendMessage: vi.fn(),
  setSetting: vi.fn(),
  composeProtectionStatus: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../stores/outbox', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/outbox')>()),
  loadOutbox: vi.fn(),
}))

vi.mock('../../stores/signatures', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/signatures')>()),
  getAccountSignatures: vi.fn().mockResolvedValue({ headerId: 0, footerId: 0 }),
}))

import Composer2 from './Composer2.svelte'
import { composeSessions, getSession, openCompose, openReply, updateCompose, type ComposeSession } from '../../stores/compose'
import { prefs } from '../../stores/prefs'
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

const missing = "It looks like you meant to attach something, but there's no attachment."

beforeEach(() => {
  document.documentElement.setAttribute('data-reduce-motion', '')
  api.sendMessage.mockReset().mockResolvedValue(7)
  api.setSetting.mockReset().mockResolvedValue(undefined)
  api.composeProtectionStatus.mockReset().mockResolvedValue({ canSign: false, canEncrypt: false, suggested: 'none', recipients: [] })
  prefs.update((p) => ({ ...p, reduceMotion: true, composeVimMode: true, sendDelaySeconds: 0, warnMissingAttachment: true, warnEmptySubject: true }))
})

afterEach(() => {
  document.documentElement.removeAttribute('data-reduce-motion')
  unsubscribe?.()
  unsubscribe = null
  composeSessions.set([])
})

function draft(over: Partial<ComposeSession>): number {
  const id = openCompose(1, 'plaintext')
  updateCompose(id, { to: 'ann@example.com', subject: 'Plan', ...over })
  return id
}

describe('the check before sending', () => {
  it('stops on a missing attachment, inline in the composer', async () => {
    const id = draft({ body: 'The plan is attached.' })
    mount(id)
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))

    expect(screen.getByRole('alert')).toHaveTextContent(missing)
    expect(screen.queryByRole('dialog', { name: /attach/i })).toBeNull()
    expect(api.sendMessage).not.toHaveBeenCalled()
  })

  it('sends anyway when asked', async () => {
    const id = draft({ body: 'Anbei die Rechnung.' })
    mount(id)
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))
    await userEvent.click(screen.getByRole('button', { name: 'Send anyway' }))

    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledOnce())
  })

  it("turns the check off with Don't ask again and sends", async () => {
    const id = draft({ body: 'See attached' })
    mount(id)
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))
    await userEvent.click(screen.getByRole('button', { name: "Don't ask again" }))

    expect(get(prefs).warnMissingAttachment).toBe(false)
    expect(api.setSetting).toHaveBeenCalledWith('warn_missing_attachment', 'false')
    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledOnce())
  })

  it('goes away once a file is attached', async () => {
    const id = draft({ body: 'The plan is attached.' })
    mount(id)
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))
    updateCompose(id, { attachments: [{ filename: 'plan.pdf', contentType: 'application/pdf', contentBase64: '', inline: false, contentId: '' }] })

    await waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
  })

  it('does not count an inline image as an attachment', async () => {
    const id = draft({
      body: 'The plan is attached.',
      attachments: [{ filename: 'logo.png', contentType: 'image/png', contentBase64: '', inline: true, contentId: 'logo' }],
    })
    mount(id)
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))
    expect(screen.getByRole('alert')).toHaveTextContent(missing)
  })

  it('ignores a mention in the quoted original of a reply', async () => {
    const original = {
      id: 42,
      accountId: 1,
      folderId: 20,
      accountEmail: 'me@example.com',
      fromName: 'Ada',
      fromAddress: 'ada@example.com',
      toAddresses: 'me@example.com',
      ccAddresses: '',
      subject: 'Invoice attached',
      date: '2026-10-01',
      bodyQuote: 'The invoice is attached.',
      messageId: '<n1@x>',
      references: [],
      inReplyTo: [],
    } as unknown as MessageDetail
    const id = openReply(original, 'plaintext', false)
    updateCompose(id, { body: `Thanks, got it.${getSession(id)?.body ?? ''}` })
    mount(id)
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))

    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledOnce())
  })

  it('asks about an empty subject and can be sent anyway', async () => {
    const id = draft({ subject: '', body: 'Hello' })
    mount(id)
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))
    expect(screen.getByRole('alert')).toHaveTextContent('This message has no subject.')
    await userEvent.click(screen.getByRole('button', { name: 'Send anyway' }))
    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledOnce())
  })

  it('asks one question after the other', async () => {
    const id = draft({ subject: '', body: 'attached' })
    mount(id)
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))
    expect(screen.getByRole('alert')).toHaveTextContent(missing)
    await userEvent.click(screen.getByRole('button', { name: 'Send anyway' }))
    expect(screen.getByRole('alert')).toHaveTextContent('This message has no subject.')
    expect(api.sendMessage).not.toHaveBeenCalled()
  })

  it('never sends without a recipient and points to the To field', async () => {
    const id = draft({ to: '', body: 'Hello' })
    mount(id)
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))
    expect(screen.getByRole('alert')).toHaveTextContent('This message has nobody to go to yet.')
    expect(screen.queryByRole('button', { name: 'Send anyway' })).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: 'Add a recipient' }))
    expect(document.activeElement?.id).toBe(`to-${id}`)
  })

  it('stays quiet with both checks off', async () => {
    prefs.update((p) => ({ ...p, warnMissingAttachment: false, warnEmptySubject: false }))
    const id = draft({ subject: '', body: 'See attached.' })
    mount(id)
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))
    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledOnce())
  })

  it('closes on Escape and leaves the message as it was', async () => {
    const id = draft({ body: 'attached' })
    mount(id)
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('alert')).toBeNull()
    expect(getSession(id)?.body).toBe('attached')
  })
})
