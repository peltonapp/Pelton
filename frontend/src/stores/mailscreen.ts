// mailscreen.ts says which mail-level screen, if any, takes the place of the
// message list and the reading pane in the new design. The sidebar stays, so
// the screen reads as part of mail rather than a window over it.

import { writable } from 'svelte/store'
import { openSpace } from './space'
import { attachmentsOpen, closeAttachments } from './attachments'

/** A screen that can fill the mail surface. */
export type MailScreen = 'newsletters'

/** The screen on the mail surface, or null for the list and reader. */
export const mailScreen = writable<MailScreen | null>(null)

/** Shows a screen on the mail surface, switching to mail first. */
export function openMailScreen(screen: MailScreen): void {
  openSpace('mail')
  closeAttachments()
  mailScreen.set(screen)
}

/** Returns the mail surface to the list and the reader. */
export function closeMailScreen(): void {
  mailScreen.set(null)
}

// the attachments browser fills the same surface, so opening it closes the
// screen rather than stacking under it.
attachmentsOpen.subscribe((open) => {
  if (open) {
    mailScreen.set(null)
  }
})
