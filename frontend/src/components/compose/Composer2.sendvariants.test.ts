import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  sendMessage: vi.fn(),
  setSetting: vi.fn(),
  composeProtectionStatus: vi.fn(),
}))

const triage = vi.hoisted(() => ({ markAnsweredDone: vi.fn() }))
const undo = vi.hoisted(() => ({ scheduleUndo: vi.fn() }))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../lib/triage', () => triage)

vi.mock('../../stores/undosend', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/undosend')>()),
  ...undo,
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
import { activeComposer } from '../../stores/composecommands'
import { extraPaletteCommands } from '../../lib/palettesources'
import { canArchiveAfter, remindAt, sendPlan } from '../../lib/sendvariants'
import { prefs } from '../../stores/prefs'
import { t } from '../../lib/i18n'
import type { MessageDetail } from '../../lib/types'

const original = {
  id: 42,
  accountId: 1,
  folderId: 20,
  accountEmail: 'me@example.com',
  fromName: 'Ada',
  fromAddress: 'ada@example.com',
  toAddresses: 'me@example.com',
  ccAddresses: '',
  subject: 'Quarterly plan',
  date: '2026-10-01',
  bodyQuote: 'Can you look at the plan?',
  messageId: '<n1@x>',
  references: [],
  inReplyTo: [],
} as unknown as MessageDetail

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

function reply(body = 'Looks good to me.'): number {
  const id = openReply(original, 'plaintext', false)
  updateCompose(id, { body: `${body}${getSession(id)?.body ?? ''}` })
  return id
}

async function openMenu(): Promise<void> {
  await userEvent.click(screen.getByRole('button', { name: 'More ways to send' }))
}

beforeEach(() => {
  document.documentElement.setAttribute('data-reduce-motion', '')
  api.sendMessage.mockReset().mockResolvedValue(7)
  api.setSetting.mockReset().mockResolvedValue(undefined)
  api.composeProtectionStatus.mockReset().mockResolvedValue({ canSign: false, canEncrypt: false, suggested: 'none', recipients: [] })
  triage.markAnsweredDone.mockReset().mockResolvedValue(undefined)
  undo.scheduleUndo.mockReset()
  prefs.update((p) => ({
    ...p,
    reduceMotion: true,
    composeVimMode: true,
    sendDelaySeconds: 0,
    warnMissingAttachment: true,
    warnEmptySubject: true,
    afterReplySend: 'nothing',
    sendRemindAfter: 'threeDays',
  }))
})

afterEach(() => {
  document.documentElement.removeAttribute('data-reduce-motion')
  unsubscribe?.()
  unsubscribe = null
  composeSessions.set([])
})

describe('sendPlan', () => {
  const now = new Date(2026, 10, 15, 16, 30)
  const base = { afterReplySend: 'nothing', sendRemindAfter: 'threeDays' } as const
  const answering = { origin: { id: 42 } } as Pick<ComposeSession, 'origin'>

  it('archives only what a reply or forward answers', () => {
    expect(canArchiveAfter({})).toBe(false)
    expect(canArchiveAfter(answering)).toBe(true)
    expect(sendPlan({}, 'archive', base, now).archiveId).toBeNull()
    expect(sendPlan(answering, 'archive', base, now).archiveId).toBe(42)
  })

  it('archives on plain send when the pref says so', () => {
    expect(sendPlan(answering, 'send', base, now).archiveId).toBeNull()
    expect(sendPlan(answering, 'send', { ...base, afterReplySend: 'archive' }, now).archiveId).toBe(42)
    expect(sendPlan({}, 'send', { ...base, afterReplySend: 'archive' }, now).archiveId).toBeNull()
  })

  it('sets a reminder only for send and remind, at the default or picked interval', () => {
    expect(sendPlan(answering, 'send', base, now).followUpAt).toBeNull()
    expect(sendPlan({}, 'remind', base, now).followUpAt).toBe(remindAt('threeDays', now).toISOString())
    expect(sendPlan({}, 'remind', base, now, 'week').followUpAt).toBe(new Date(2026, 10, 22, 9, 0).toISOString())
  })
})

describe('send and archive', () => {
  it('is not offered for a new message', async () => {
    const id = openCompose(1, 'plaintext')
    updateCompose(id, { to: 'ann@example.com', subject: 'Hello', body: 'Hi' })
    mount(id)
    await openMenu()

    expect(screen.queryByRole('menuitem', { name: /Send and archive/ })).toBeNull()
    expect(extraPaletteCommands(get(t)).find((c) => c.id === 'compose:compose-send-archive')).toBeUndefined()
  })

  it('archives the answered conversation once the reply is queued', async () => {
    const id = reply()
    mount(id)
    await openMenu()
    await userEvent.click(screen.getByRole('menuitem', { name: /Send and archive/ }))

    await waitFor(() => expect(triage.markAnsweredDone).toHaveBeenCalledWith(42))
    expect(api.sendMessage).toHaveBeenCalledOnce()
    expect(getSession(id)).toBeUndefined()
  })

  it('does not archive while a send check holds the reply, and does once sent anyway', async () => {
    const id = reply('The figures are attached.')
    mount(id)
    await openMenu()
    await userEvent.click(screen.getByRole('menuitem', { name: /Send and archive/ }))

    expect(screen.getByRole('alert')).toBeInTheDocument()
    expect(api.sendMessage).not.toHaveBeenCalled()
    expect(triage.markAnsweredDone).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Send anyway' }))
    await waitFor(() => expect(triage.markAnsweredDone).toHaveBeenCalledWith(42))
  })

  it('does not archive when the send fails', async () => {
    api.sendMessage.mockRejectedValue(new Error('outbox is down'))
    const id = reply()
    mount(id)
    await openMenu()
    await userEvent.click(screen.getByRole('menuitem', { name: /Send and archive/ }))

    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledOnce())
    expect(getSession(id)).toBeDefined()
    expect(triage.markAnsweredDone).not.toHaveBeenCalled()
  })

  it('waits for the undo window to pass', async () => {
    prefs.update((p) => ({ ...p, sendDelaySeconds: 10 }))
    const id = reply()
    mount(id)
    await openMenu()
    await userEvent.click(screen.getByRole('menuitem', { name: /Send and archive/ }))

    await waitFor(() => expect(undo.scheduleUndo).toHaveBeenCalledOnce())
    expect(triage.markAnsweredDone).not.toHaveBeenCalled()

    const committed = undo.scheduleUndo.mock.calls[0][3] as () => void
    committed()
    await waitFor(() => expect(triage.markAnsweredDone).toHaveBeenCalledWith(42))
  })

  it('runs from its shortcut', async () => {
    const id = reply()
    mount(id)
    screen.getByRole('textbox', { name: 'Subject' }).focus()

    await userEvent.keyboard('{Control>}{Shift>}{Enter}{/Shift}{/Control}')

    await waitFor(() => expect(triage.markAnsweredDone).toHaveBeenCalledWith(42))
  })

  it('is what plain send does for a reply when the pref is set', async () => {
    prefs.update((p) => ({ ...p, afterReplySend: 'archive' }))
    const id = reply()
    mount(id)
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))

    await waitFor(() => expect(triage.markAnsweredDone).toHaveBeenCalledWith(42))
  })

  it('leaves plain send alone by default', async () => {
    const id = reply()
    mount(id)
    await userEvent.click(screen.getByRole('button', { name: 'Send' }))

    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledOnce())
    await waitFor(() => expect(getSession(id)).toBeUndefined())
    expect(triage.markAnsweredDone).not.toHaveBeenCalled()
  })
})

describe('send and remind', () => {
  function draft(): number {
    const id = openCompose(1, 'plaintext')
    updateCompose(id, { to: 'ann@example.com', subject: 'Plan', body: 'Here is the plan.' })
    return id
  }

  it('sends with a reminder at the picked interval', async () => {
    const id = draft()
    mount(id)
    await openMenu()
    await userEvent.click(screen.getByRole('menuitem', { name: 'Send and remind, In a week' }))

    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledOnce())
    const at = new Date(api.sendMessage.mock.calls[0][0].followUpAt)
    expect(at.getHours()).toBe(9)
    expect(Math.round((at.getTime() - Date.now()) / 86_400_000)).toBeGreaterThanOrEqual(6)
    expect(triage.markAnsweredDone).not.toHaveBeenCalled()
  })

  it('uses the default interval from its shortcut', async () => {
    prefs.update((p) => ({ ...p, sendRemindAfter: 'tomorrow' }))
    const id = draft()
    mount(id)
    screen.getByRole('textbox', { name: 'Subject' }).focus()

    await userEvent.keyboard('{Control>}{Alt>}{Enter}{/Alt}{/Control}')

    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledOnce())
    expect(api.sendMessage.mock.calls[0][0].followUpAt).toBe(remindAt('tomorrow', new Date()).toISOString())
  })

  it('keeps the reminder the session had when the send fails', async () => {
    api.sendMessage.mockRejectedValue(new Error('outbox is down'))
    const id = draft()
    mount(id)
    await openMenu()
    await userEvent.click(screen.getByRole('menuitem', { name: 'Send and remind, Tomorrow' }))

    await waitFor(() => expect(api.sendMessage).toHaveBeenCalledOnce())
    await waitFor(() => expect(getSession(id)?.followUpAt).toBeUndefined())
  })

  it('is listed in the command palette with its key', () => {
    const id = draft()
    mount(id)
    expect(get(activeComposer)).toBe(id)

    const entry = extraPaletteCommands(get(t)).find((c) => c.id === 'compose:compose-send-remind')
    expect(entry?.label).toBe('Send and remind')
    expect(entry?.hint).toBeTruthy()
  })
})
