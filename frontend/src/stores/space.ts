// space.ts holds which top-level area of the app is showing in the new design:
// mail, contacts, the calendar or tasks. the rail, the sidebar footer switcher and the
// top bar tabs all read and write this one store.

import { writable } from 'svelte/store'
import type { Space } from '../lib/types'

/** The area currently on screen. */
export const space = writable<Space>('mail')

/** Shows the given area. */
export function openSpace(next: Space): void {
  space.set(next)
}

/** What a space does for an app action; returning false passes it on. */
export type SpaceActionHandler = () => boolean | void

const spaceActions = new Map<Space, Map<string, SpaceActionHandler>>()

// actions that only mean something with the mail panes on screen. another
// space that does not take one over swallows it, so a shortcut never acts on
// a message the user cannot see.
const mailOnlyActions = new Set<string>([
  'reply',
  'reply-all',
  'forward',
  'mark-read',
  'mark-unread',
  'flag',
  'flag-color',
  'snooze',
  'download-offline',
  'remove-offline',
  'delete-message',
  'archive',
  'toggle-spam',
  'unsubscribe',
  'open-in-tab',
  'close-tab',
  'move-to',
  'mark-vip',
  'export-pdf',
  'export-eml',
  'next-pane',
  'prev-pane',
  'focus-sidebar',
  'focus-list',
  'focus-reader',
  'new-view',
  'next-view',
  'prev-view',
  'empty-trash',
  'new-folder',
  'rename-folder',
  'delete-folder',
  'toggle-sender-panel',
  'create-task',
  'done',
  'toggle-pin',
  'toggle-mute',
  'edit-note',
])

/** Reports whether an action only means something with the mail panes on screen. */
export function isMailOnlyAction(action: string): boolean {
  return mailOnlyActions.has(action)
}

/**
 * Lets a space answer app actions while it is on screen: compose becomes "new
 * event" in the calendar, search focuses the space's own search, and so on.
 * Returns the function that takes the handlers away again.
 */
export function registerSpaceActions(key: Space, handlers: Record<string, SpaceActionHandler>): () => void {
  const map = new Map(Object.entries(handlers))
  spaceActions.set(key, map)
  return () => {
    if (spaceActions.get(key) === map) {
      spaceActions.delete(key)
    }
  }
}

const mountWaitFrames = 180

/**
 * Shows a space and runs one of its actions there. The space's screen loads
 * on first use, so the action waits for the screen to register its handlers.
 */
export function runInSpace(key: Space, action: string): void {
  space.set(key)
  let frames = 0
  const attempt = (): void => {
    const handler = spaceActions.get(key)?.get(action)
    if (handler) {
      handler()
    } else if (frames < mountWaitFrames) {
      frames += 1
      requestAnimationFrame(attempt)
    }
  }
  attempt()
}

/**
 * Offers an action to the space on screen. True means it is dealt with: the
 * space handled it, or it is a mail action that has no meaning there.
 */
export function runSpaceAction(key: Space, action: string): boolean {
  if (key === 'mail') {
    return false
  }
  const handler = spaceActions.get(key)?.get(action)
  if (handler && handler() !== false) {
    return true
  }
  return mailOnlyActions.has(action)
}
