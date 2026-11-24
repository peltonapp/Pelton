// notemail.ts turns mail into links a note can hold: mail dropped on the
// Notes entry of the switcher becomes links in the open note, or a new note
// about the first message when no note is open.

import { get } from 'svelte/store'
import { mailEventSource } from './api'
import { messageIdFromMid, peltonMarkdownLink } from './peltonlinks'
import { t } from './i18n'
import { activeNote, newNoteAbout, noteInsert } from '../stores/notes'
import { space } from '../stores/space'
import { errorMessage, toastError, toastInfo } from '../stores/toast'

/** A mail as a note links to it. */
export interface MailLink {
  /** The Message-ID. */
  id: string
  subject: string
  /** The Markdown link. */
  link: string
}

/** The links to the given messages; messages without a Message-ID are left out. */
export async function mailLinks(messageIds: number[]): Promise<MailLink[]> {
  const out: MailLink[] = []
  for (const id of messageIds) {
    const src = await mailEventSource(id)
    if (!src.link) {
      continue
    }
    const mid = messageIdFromMid(src.link)
    const subject = src.subject || get(t)('notes.object.mailUntitled')
    out.push({ id: mid, subject, link: peltonMarkdownLink('mail', mid, subject) })
  }
  return out
}

/**
 * Takes mail dropped on Notes: with a note open in the notes space, links
 * to the messages go in at its cursor; otherwise a note about the first one
 * is made.
 */
export async function dropMailOnNotes(messageIds: number[]): Promise<void> {
  try {
    const links = await mailLinks(messageIds)
    if (!links.length) {
      toastInfo(get(t)('notes.mailNoId'))
      return
    }
    if (get(space) === 'notes' && get(activeNote)) {
      noteInsert.set(links.length === 1 ? links[0].link : links.map((l) => `- ${l.link}`).join('\n'))
      return
    }
    await newNoteAbout({ kind: 'mail', id: links[0].id, title: links[0].subject })
  } catch (err) {
    toastError(errorMessage(err))
  }
}
