// mailtohold.ts holds a mailto: link until compose can open for it. That needs
// a mailbox to send from, and nothing full screen in the way: the add-mailbox
// wizard and onboarding cover the window, so a compose opened under them would
// sit hidden behind the folder picker or the next onboarding step.
//
// Adding a mailbox is therefore not the moment to open it. The flow that added
// it calls release() when it closes, after the sidebar has re-read the
// accounts: compose sends from a mailbox the sidebar lists. A flow that does
// not call it (the wizard inside Settings) is covered by accountsChanged,
// which releases once the sidebar goes from no mailbox to some.

import type { MailtoDraft } from './events'

export interface MailtoHold {
  /** Opens compose for a link now, or holds it until release() can. */
  offer(draft: MailtoDraft): void
  /** Opens a held link if a mailbox exists and nothing covers the window. */
  release(): void
  /** Releases once pending settles, whether or not it succeeded. */
  releaseAfter(pending: Promise<unknown>): Promise<void>
  /** Takes the sidebar's mailbox count; releases when it goes from 0 to more. */
  accountsChanged(count: number): void
}

export function createMailtoHold(deps: {
  /** The mailbox compose would send from, or null when there is none. */
  accountId: () => number | null
  /** Whether a full-screen flow (wizard, onboarding) is open. */
  covered: () => boolean
  open: (accountId: number, draft: MailtoDraft) => void
}): MailtoHold {
  let held: MailtoDraft | null = null
  let accounts = 0

  function tryOpen(draft: MailtoDraft): boolean {
    const id = deps.accountId()
    if (id === null || deps.covered()) {
      return false
    }
    deps.open(id, draft)
    return true
  }

  return {
    offer(draft) {
      if (!tryOpen(draft)) {
        held = draft
      }
    },
    release() {
      if (held && tryOpen(held)) {
        held = null
      }
    },
    async releaseAfter(pending) {
      try {
        await pending
      } catch {
        // a failed refresh leaves the mailbox list as it was; release decides.
      }
      this.release()
    },
    accountsChanged(count) {
      const before = accounts
      accounts = count
      if (before === 0 && count > 0) {
        this.release()
      }
    },
  }
}
