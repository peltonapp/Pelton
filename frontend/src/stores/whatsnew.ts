// whatsnew.ts (store) knows whether the running version has notes the user
// has not opened yet, which the settings entry marks with a dot, and whether
// the What's new page is open. Nothing is fetched: the notes ship with the app
// in lib/whatsnew and the seen marker sits in the local settings table.

import { get, writable } from 'svelte/store'
import { appVersion, getSetting, setSetting, SettingKeys } from '../lib/api'
import { hasUnseen, releaseFor, seenKey, type WhatsNewRelease } from '../lib/whatsnew'

/** The notes for the running version, or null when it has none. */
export const whatsNewRelease = writable<WhatsNewRelease | null>(null)

/** The running version has notes that have not been opened yet. */
export const whatsNewUnseen = writable(false)

/** The What's new page is showing. */
export const whatsNewOpen = writable(false)

let ready: Promise<void> | null = null

/** Reads the running version and the seen marker. Safe to call more than once. */
export function initWhatsNew(): Promise<void> {
  ready ??= (async () => {
    let version = ''
    try {
      version = await appVersion()
    } catch {
      version = ''
    }
    let stored = ''
    try {
      const r = await getSetting(SettingKeys.lastSeenVersion)
      stored = r.found ? r.value : ''
    } catch {
      stored = ''
    }
    whatsNewRelease.set(releaseFor(version))
    whatsNewUnseen.set(hasUnseen(stored, version))
  })()
  return ready
}

/** Records the running version's notes as seen. */
export async function markWhatsNewSeen(): Promise<void> {
  await initWhatsNew()
  const release = get(whatsNewRelease)
  whatsNewUnseen.set(false)
  if (release) {
    await setSetting(SettingKeys.lastSeenVersion, seenKey(release)).catch(() => {})
  }
}

/** Opens the What's new page, which counts as having seen it. */
export function openWhatsNew(): void {
  whatsNewOpen.set(true)
  void markWhatsNewSeen()
}
