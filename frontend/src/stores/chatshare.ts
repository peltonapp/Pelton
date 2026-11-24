// chatshare.ts holds the mail that is being shared to a chat room, null while
// the share dialog is closed.

import { writable } from 'svelte/store'
import type { MessageDetail } from '../lib/types'

/** The message the share dialog is open for, or null. */
export const shareToChat = writable<MessageDetail | null>(null)

/** Opens the share dialog for a message. */
export function openShareToChat(message: MessageDetail): void {
  shareToChat.set(message)
}

/** Closes the share dialog. */
export function closeShareToChat(): void {
  shareToChat.set(null)
}
