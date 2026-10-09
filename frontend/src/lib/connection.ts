import type { Account } from './types'

/**
 * connectionSummary says how an account reaches its server, for the mailbox
 * list: protocol plus host and port. The local account has no server, so it
 * gets localLabel instead.
 */
export function connectionSummary(account: Pick<Account, 'imapHost' | 'imapPort' | 'local'>, localLabel: string): string {
  if (account.local) {
    return localLabel
  }
  return `IMAP · ${account.imapHost}:${account.imapPort}`
}
