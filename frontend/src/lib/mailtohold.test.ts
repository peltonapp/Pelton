import { describe, expect, it, vi } from 'vitest'
import { createMailtoHold } from './mailtohold'
import type { MailtoDraft } from './events'

const draft: MailtoDraft = { to: 'a@example.org', cc: '', bcc: '', subject: 'hi', body: '' }

function setup() {
  const state = { accountId: null as number | null, covered: false }
  const open = vi.fn()
  const hold = createMailtoHold({ accountId: () => state.accountId, covered: () => state.covered, open })
  return { state, open, hold }
}

describe('a mailto link waiting for a mailbox', () => {
  it('opens at once when a mailbox exists and nothing covers the window', () => {
    const { state, open, hold } = setup()
    state.accountId = 3

    hold.offer(draft)

    expect(open).toHaveBeenCalledWith(3, draft)
  })

  it('waits for the add-mailbox wizard to close, not for the mailbox to be added', () => {
    const { state, open, hold } = setup()
    state.covered = true
    hold.offer(draft)

    // the wizard added a mailbox but is still showing its folder picker.
    state.accountId = 3
    hold.release()
    expect(open).not.toHaveBeenCalled()

    state.covered = false
    hold.release()
    expect(open).toHaveBeenCalledExactlyOnceWith(3, draft)
    hold.release()
    expect(open).toHaveBeenCalledOnce()
  })

  it('keeps the link when the wizard closes without adding a mailbox', () => {
    const { state, open, hold } = setup()
    hold.offer(draft)

    hold.release()
    expect(open).not.toHaveBeenCalled()

    state.accountId = 5
    hold.release()
    expect(open).toHaveBeenCalledWith(5, draft)
  })

  // closeWizard: the wizard is gone at once, but compose needs the mailbox it
  // added, which the sidebar lists only once its refresh lands.
  it('waits for the sidebar refresh the wizard close starts', async () => {
    const { state, open, hold } = setup()
    hold.offer(draft)
    let refreshed!: () => void
    const refresh = new Promise<void>((r) => (refreshed = r))

    const released = hold.releaseAfter(refresh)
    await Promise.resolve()
    expect(open).not.toHaveBeenCalled()

    state.accountId = 3
    refreshed()
    await released
    expect(open).toHaveBeenCalledExactlyOnceWith(3, draft)
  })

  it('releases even when that refresh fails', async () => {
    const { state, open, hold } = setup()
    hold.offer(draft)
    state.accountId = 3

    await hold.releaseAfter(Promise.reject(new Error('offline')))

    expect(open).toHaveBeenCalledWith(3, draft)
  })

  // the wizard inside Settings closes without telling App, so the first
  // mailbox showing up in the sidebar is what lets the link through.
  it('opens when the sidebar gets its first mailbox', () => {
    const { state, open, hold } = setup()
    hold.accountsChanged(0)
    hold.offer(draft)

    state.accountId = 4
    hold.accountsChanged(1)

    expect(open).toHaveBeenCalledExactlyOnceWith(4, draft)
  })

  it('does not count a second mailbox as the first', () => {
    const { state, open, hold } = setup()
    state.accountId = 4
    hold.accountsChanged(1)
    state.covered = true
    hold.offer(draft)
    state.covered = false

    hold.accountsChanged(2)

    expect(open).not.toHaveBeenCalled()
  })
})
