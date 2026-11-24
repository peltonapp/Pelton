// threads.ts holds the conversations the new design has opened: the members of
// a thread the list expanded or the reader is showing, and the full messages
// of the reader's cards. Both are caches over the backend and are dropped
// whenever the list reloads, so a regrouped or repaired thread is read again.

import { get, writable } from 'svelte/store'
import type { MessageDetail, MessageSummary, ThreadMessage, UIPrefs } from '../lib/types'
import { getMessage, getThread } from '../lib/api'

/** threadedOn reports whether the list is grouped into conversations. */
export function threadedOn(p: Pick<UIPrefs, 'design' | 'threadedList'>): boolean {
  return p.design !== 'classic' && p.threadedList
}

/** The loaded members of each thread, oldest first, by thread id. */
export const threadMessages = writable<Map<number, ThreadMessage[]>>(new Map())

/** The full messages the conversation reader has opened, by message id. */
export const cardDetails = writable<Map<number, MessageDetail>>(new Map())

// a thread asked for twice while its first read is out shares that read.
const inflight = new Map<number, Promise<ThreadMessage[]>>()
// bumped by invalidateThreads, so a read that started before it never fills
// the cache it cleared.
let generation = 0

/**
 * loadThread returns a thread's members, from the cache when it holds them.
 * threadId 0 has no members to read and returns nothing.
 */
export async function loadThread(threadId: number): Promise<ThreadMessage[]> {
  if (threadId === 0) {
    return []
  }
  const cached = get(threadMessages).get(threadId)
  if (cached) {
    return cached
  }
  const running = inflight.get(threadId)
  if (running) {
    return running
  }
  const started = generation
  const read = getThread(threadId)
    .then((thread) => {
      const messages = thread.messages ?? []
      if (started === generation) {
        threadMessages.update((m) => new Map(m).set(threadId, messages))
      }
      return messages
    })
    .finally(() => inflight.delete(threadId))
  inflight.set(threadId, read)
  return read
}

/** loadCard returns the full message for a reader card, cached. */
export async function loadCard(id: number): Promise<MessageDetail> {
  const cached = get(cardDetails).get(id)
  if (cached) {
    return cached
  }
  const detail = await getMessage(id)
  cardDetails.update((m) => new Map(m).set(id, detail))
  return detail
}

/**
 * refreshCard reads a cached card again, after its body arrived: a card that
 * showed a stub's preview then shows the whole message.
 */
export async function refreshCard(id: number): Promise<void> {
  if (!get(cardDetails).has(id)) {
    return
  }
  try {
    const detail = await getMessage(id)
    cardDetails.update((m) => (m.has(id) ? new Map(m).set(id, detail) : m))
  } catch {
    // the card keeps what it showed; opening it again reads it anew.
  }
}

/** invalidateThreads drops every cached thread and card. */
export function invalidateThreads(): void {
  generation++
  inflight.clear()
  threadMessages.set(new Map())
  cardDetails.set(new Map())
}

/**
 * threadSeenOf returns the cached read state of a message in a loaded thread,
 * or undefined when no loaded thread holds it.
 */
export function threadSeenOf(id: number): boolean | undefined {
  for (const members of get(threadMessages).values()) {
    const m = members.find((x) => x.id === id)
    if (m) {
      return m.seen
    }
  }
  return undefined
}

/** patchThreadMessage applies a change to a message wherever it is cached. */
export function patchThreadMessage(id: number, patch: Partial<MessageSummary>): void {
  threadMessages.update((map) => {
    let changed = false
    const next = new Map(map)
    for (const [threadId, members] of map) {
      if (members.some((m) => m.id === id)) {
        next.set(threadId, members.map((m) => (m.id === id ? { ...m, ...patch } : m)))
        changed = true
      }
    }
    return changed ? next : map
  })
  cardDetails.update((map) => {
    const detail = map.get(id)
    return detail ? new Map(map).set(id, { ...detail, ...patch }) : map
  })
}

/** dropThreadMessage removes a message that left its folder from the caches. */
export function dropThreadMessage(id: number): void {
  threadMessages.update((map) => {
    let changed = false
    const next = new Map(map)
    for (const [threadId, members] of map) {
      if (members.some((m) => m.id === id)) {
        next.set(threadId, members.filter((m) => m.id !== id))
        changed = true
      }
    }
    return changed ? next : map
  })
  forgetCard(id)
}

/** forgetCard drops a card's cached message, so it is read again. */
export function forgetCard(id: number): void {
  cardDetails.update((map) => {
    if (!map.has(id)) {
      return map
    }
    const next = new Map(map)
    next.delete(id)
    return next
  })
}

/** setCardBody swaps the rendered body of a cached card. */
export function setCardBody(id: number, html: string): void {
  cardDetails.update((map) => {
    const detail = map.get(id)
    return detail ? new Map(map).set(id, { ...detail, bodyHtmlSafe: html }) : map
  })
}
