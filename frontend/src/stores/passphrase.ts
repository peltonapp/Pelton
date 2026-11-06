// passphrase.ts drives the prompt for a locked PGP private key (#192). The
// prompt is raised from wherever a locked key is needed (settings, opening a
// sealed draft), so the request goes through this store rather than each
// caller rendering its own copy of the dialog.
//
// The passphrase itself never lands in this store: the dialog hands it straight
// to the backend, which verifies it against the key and holds it for the
// session. Nothing here is persisted.

import { writable } from 'svelte/store'
import type { PGPKey } from '../lib/types'

/** An open request to unlock one key. */
export interface PassphraseRequest {
  key: PGPKey
  /** Called after the backend accepted the passphrase. */
  onUnlocked: () => void
}

export const passphraseRequest = writable<PassphraseRequest | null>(null)

/** Prompts for a key's passphrase, calling back once it is accepted. */
export function openPassphrase(key: PGPKey, onUnlocked: () => void = () => {}): void {
  passphraseRequest.set({ key, onUnlocked })
}

/** Dismisses the prompt without unlocking. */
export function closePassphrase(): void {
  passphraseRequest.set(null)
}
