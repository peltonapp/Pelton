import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  sendMessage: vi.fn(),
  saveDraft: vi.fn(),
  deleteDraft: vi.fn(),
  uploadDraft: vi.fn(),
  removeServerDraft: vi.fn(),
  composeProtectionStatus: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../stores/outbox', () => ({
  loadOutbox: vi.fn(),
}))

vi.mock('../../stores/signatures', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/signatures')>()),
  getAccountSignatures: vi.fn().mockResolvedValue({ headerId: 0, footerId: 0 }),
}))

import Compose from './Compose.svelte'
import { composeSessions, getSession, openCompose, openDraftSession, updateCompose, type ComposeSession } from '../../stores/compose'
import type { ComposeRequest } from '../../lib/types'
import { toasts } from '../../stores/toast'

let unsubscribe: (() => void) | null = null

// openPane renders a compose pane the way App does, re-rendered from the store
// so an edit made inside the pane reaches its session.
function openPane(patch: Partial<ComposeSession> = {}): number {
  const id = openCompose(1, 'plaintext')
  updateCompose(id, { signaturesApplied: true, to: 'ann@example.com', subject: 'Hi', ...patch })
  return showPane(id)
}

// showPane renders the pane of a session already in the store, in place of any
// pane rendered before.
function showPane(id: number): number {
  unsubscribe?.()
  cleanup()
  const { rerender } = render(Compose, { props: { session: getSession(id)! } })
  unsubscribe = composeSessions.subscribe((list) => {
    const next = list.find((s) => s.id === id)
    if (next) {
      void rerender({ session: next })
    }
  })
  return id
}

const click = (name: string): Promise<void> => userEvent.click(screen.getByRole('button', { name }))
const toastMessages = (): string[] => get(toasts).map((t) => t.message)

async function saveUploaded(id: number): Promise<void> {
  await click('Save draft')
  await vi.waitFor(() => expect(getSession(id)?.draftMessageId).toBe('<d1@pelton>'))
}

async function discard(): Promise<void> {
  await click('Close compose')
  await click('Discard')
}

// clickDuringSave clicks Save draft, then `next` while the first local save or
// upload is still in flight; that step finishes when the returned function is called.
async function clickDuringSave(step: 'save' | 'upload', next: string): Promise<() => void> {
  openPane()
  let finish!: () => void
  const fn = step === 'save' ? api.saveDraft : api.uploadDraft
  fn.mockReturnValueOnce(new Promise((r) => (finish = () => r(step === 'save' ? 7 : '<d1@pelton>'))))
  await click('Save draft')
  await click(next)
  return finish
}

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.sendMessage.mockResolvedValue(1)
  api.saveDraft.mockResolvedValue(7)
  api.uploadDraft.mockResolvedValue('<d1@pelton>')
  api.composeProtectionStatus.mockResolvedValue({ canSign: true, canEncrypt: true, suggested: 'none' })
})

afterEach(() => {
  unsubscribe?.()
  unsubscribe = null
  composeSessions.set([])
  toasts.set([])
})

describe('saving a draft to the server', () => {
  it('uploads after the local save and remembers the server copy', async () => {
    await saveUploaded(openPane())

    expect(api.saveDraft).toHaveBeenCalledOnce()
    expect(api.uploadDraft).toHaveBeenCalledWith(7)
    expect(api.saveDraft.mock.invocationCallOrder[0]).toBeLessThan(api.uploadDraft.mock.invocationCallOrder[0])
  })

  it.each(['save', 'upload'] as const)('makes a second quick save wait for the first %s and carry its ids', async (step) => {
    const finish = await clickDuringSave(step, 'Save draft')
    expect(api.saveDraft).toHaveBeenCalledOnce()
    finish()

    await vi.waitFor(() => expect(api.uploadDraft).toHaveBeenCalledTimes(2))
    expect(api.saveDraft.mock.calls).toEqual([
      [0, expect.anything()],
      [7, expect.objectContaining({ draftMessageId: '<d1@pelton>' })],
    ])
  })

  it.each(['save', 'upload'] as const)('sends with the server copy of a draft whose %s is still running', async (step) => {
    const finish = await clickDuringSave(step, 'Send')
    expect(api.sendMessage).not.toHaveBeenCalled()
    finish()

    await vi.waitFor(() => expect(api.sendMessage).toHaveBeenCalledOnce())
    expect(api.sendMessage.mock.calls[0][0].draftMessageId).toBe('<d1@pelton>')
    expect(api.deleteDraft).toHaveBeenCalledWith(7)
  })

  it('keeps an encrypted draft on this device', async () => {
    openPane({ protection: 'encrypt' })

    await click('Save draft')

    await vi.waitFor(() => expect(toastMessages()).toContain('Encrypted drafts are not uploaded to the server.'))
    expect(api.saveDraft).toHaveBeenCalledOnce()
    expect(api.uploadDraft).not.toHaveBeenCalled()
  })

  it('reports a failed upload without hiding the local save', async () => {
    openPane()
    api.uploadDraft.mockRejectedValue(new Error('no Drafts folder'))

    await click('Save draft')

    await vi.waitFor(() =>
      expect(toastMessages()).toEqual(['Draft saved.', "Saved on this device, but couldn't upload to the server: no Drafts folder"]),
    )
  })

  it('brings back a pane closed by save and close when the upload fails, still saved', async () => {
    const id = openPane()
    await saveUploaded(id)
    api.uploadDraft.mockRejectedValueOnce(new Error('no Drafts folder'))
    updateCompose(id, { subject: 'Keep me' })

    await click('Close compose')
    await click('Save to drafts')

    await vi.waitFor(() => expect(get(composeSessions)).toHaveLength(1))
    const restored = get(composeSessions)[0]
    expect(restored).toMatchObject({ subject: 'Keep me', draftId: 7, draftMessageId: '<d1@pelton>' })
    showPane(restored.id)
    await click('Close compose')
    expect(get(composeSessions)).toEqual([])
  })

  it('brings back the pane already editing the draft rather than a second one when the upload fails', async () => {
    let fail!: () => void
    api.uploadDraft.mockReturnValueOnce(new Promise((_, reject) => (fail = () => reject(new Error('no Drafts folder')))))
    openPane()
    await click('Close compose')
    await click('Save to drafts')
    await vi.waitFor(() => expect(api.uploadDraft).toHaveBeenCalled())
    const request = { to: [], subject: 'Hi', text: '', html: '' } as unknown as ComposeRequest
    const reopened = openDraftSession({ id: 7, savedAt: '', locked: false, accountId: 1, protection: 'none', request })
    updateCompose(reopened, { minimized: true })

    fail()

    await vi.waitFor(() => expect(toastMessages()).toContain("Saved on this device, but couldn't upload to the server: no Drafts folder"))
    expect(get(composeSessions)).toMatchObject([{ id: reopened, minimized: false }])
  })

  it.each([
    ['never saved', async () => void openPane()],
    ['saved in this pane', async () => saveUploaded(openPane())],
    ['opened from Drafts', async () => void openPane({ draftId: 7, draftMessageId: '<d1@pelton>', saved: true })],
  ])('discarding the edits to a draft %s closes it and deletes nothing', async (_, open) => {
    await open()
    updateCompose(get(composeSessions)[0].id, { subject: 'Changed' })

    await discard()

    await vi.waitFor(() => expect(get(composeSessions)).toEqual([]))
    expect(api.deleteDraft).not.toHaveBeenCalled()
    expect(api.removeServerDraft).not.toHaveBeenCalled()
  })

  it('does not bring a discarded pane back when its running upload fails', async () => {
    let fail!: () => void
    api.uploadDraft.mockReturnValueOnce(new Promise((_, reject) => (fail = () => reject(new Error('offline')))))
    const id = openPane()
    await click('Save draft')
    updateCompose(id, { subject: 'Changed' })

    await discard()
    fail()

    await vi.waitFor(() => expect(toastMessages().some((m) => m.includes('offline'))).toBe(true))
    await new Promise((r) => setTimeout(r, 0))
    expect(get(composeSessions)).toEqual([])
  })

  it.each([
    ['saved in this pane', async () => saveUploaded(openPane())],
    ['opened from Drafts', async () => void openPane({ draftId: 7, draftMessageId: '<d1@pelton>', saved: true })],
  ])('closes a draft %s without asking until it changes', async (_, open) => {
    await open()
    const id = get(composeSessions)[0].id

    await click('Close compose')
    expect(getSession(id)).toBeUndefined()
    expect(screen.queryByRole('button', { name: 'Discard' })).toBeNull()
  })

  it('removes the plaintext server copy once the draft is switched to encrypted', async () => {
    const id = openPane()
    await saveUploaded(id)

    updateCompose(id, { protection: 'encrypt' })
    await click('Save draft')

    await vi.waitFor(() => {
      expect(api.removeServerDraft).toHaveBeenCalledWith(1, '<d1@pelton>')
      expect(getSession(id)?.draftMessageId).toBe('')
    })
    expect(api.uploadDraft).toHaveBeenCalledOnce()
  })

  it('keeps the protection an opened draft was saved with', async () => {
    api.composeProtectionStatus.mockResolvedValue({ canSign: true, canEncrypt: true, suggested: 'encrypt' })
    const id = openPane({ draftId: 7, saved: true, protection: 'none' })

    await vi.waitFor(() => expect(api.composeProtectionStatus).toHaveBeenCalled())
    await new Promise((r) => setTimeout(r, 0))
    expect(getSession(id)?.protection).toBe('none')
  })
})

describe('opening a draft', () => {
  it('brings back the pane already editing it instead of opening a second', () => {
    const request = { to: [], subject: 'Hi', text: 'body', html: '' } as unknown as ComposeRequest
    const draft = { id: 9, savedAt: '', locked: false, accountId: 1, protection: 'none', request }
    const first = openDraftSession(draft)
    updateCompose(first, { minimized: true })

    expect(openDraftSession(draft)).toBe(first)
    expect(get(composeSessions)).toHaveLength(1)
    expect(getSession(first)?.minimized).toBe(false)
  })
})
