// chatavailable.ts says whether chat features may be offered elsewhere in the
// app: the user turned chat on and an account is signed in.

import { derived } from 'svelte/store'
import { prefs } from '../stores/prefs'
import { chatStatus, openDirectChat, chatError } from '../stores/chat'
import { toastError } from '../stores/toast'

/** True while chat is turned on and an account is signed in. */
export const chatAvailable = derived([prefs, chatStatus], ([$prefs, $status]) => $prefs.chatEnabled && $status.state !== 'signedOut')

/** Opens the direct chat with a Matrix user and toasts the reason when that fails. */
export async function chatWith(matrixId: string): Promise<void> {
  try {
    await openDirectChat(matrixId)
  } catch (err) {
    toastError(chatError(err))
  }
}
