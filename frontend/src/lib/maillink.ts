// maillink.ts links an event back to a mail: a line naming the mail with a
// mid: URL (RFC 2392) built from its Message-ID, which survives a sync to other
// calendar apps, and the way back from such a link to the mail in Mail.

import { get } from 'svelte/store'
import { findMessageByLink } from './api'
import { t } from './i18n'
import { sidebar } from '../stores/accounts'
import { revealMessage } from '../stores/selection'
import { openSpace } from '../stores/space'
import { errorMessage, toastError, toastInfo } from '../stores/toast'
import type { MailEventSource } from './types'

const midPattern = /\bmid:[^\s<>"')\]]+/gi

/** The mid: links in a text, in order and without repeats. */
export function mailLinks(text: string): string[] {
  return [...new Set(text.match(midPattern) ?? [])]
}

/** The lines an event's notes get for a mail: who sent what and when, and the link. */
export function mailLinkLines(mail: MailEventSource, translate: (key: string) => string = get(t)): string {
  const from = mail.fromName ? `${mail.fromName} <${mail.fromAddress}>` : mail.fromAddress
  const when = mail.date ? new Date(mail.date).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }) : ''
  const line = translate('calendar.mailLine').replace('{from}', from).replace('{subject}', mail.subject || translate('calendar.untitled')).replace('{date}', when)
  return mail.link ? `${line}\n${mail.link}` : line
}

/** Opens the mail a mid: link names in Mail, or says that it is gone. */
export async function openMailLink(link: string): Promise<void> {
  try {
    const ref = await findMessageByLink(link)
    if (!ref.found) {
      toastInfo(get(t)('calendar.mailGone'))
      return
    }
    openSpace('mail')
    revealMessage(ref.id, ref.accountId, ref.folderId, get(sidebar).data ?? null)
  } catch (err) {
    toastError(errorMessage(err))
  }
}
