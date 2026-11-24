// chattimeline.ts lays a chat timeline out as rows: day separators, the
// unread marker, and messages that know whether they continue the previous
// sender's group (and so draw without their own avatar and name).

import type { ChatMessage } from './types'

/** How close two messages of one sender must be to share a group. */
export const groupWindowMs = 5 * 60 * 1000

/** A pending message as the timeline lays it out. */
export interface PendingRow {
  localId: string
  text: string
  createdAt: number
}

/** One row of a laid out timeline. */
export type TimelineItem =
  | { type: 'day'; key: string; ts: number }
  | { type: 'unread'; key: string }
  | { type: 'message'; key: string; message: ChatMessage; continued: boolean }
  | { type: 'pending'; key: string; localId: string; continued: boolean }

/** What buildTimeline needs to know besides the messages. */
export interface TimelineOptions {
  /** The event the user had read up to when the room was opened. */
  unreadAfter: string
  /** The user's own Matrix id, for pending rows. */
  ownId: string
  /** Messages being sent, oldest first. */
  pending?: PendingRow[]
}

/** The local calendar day of a timestamp, as a comparable key. */
export function dayKey(ts: number): string {
  const d = new Date(ts)
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`
}

/** Rows that show a person's words, which group with each other. */
function groups(m: ChatMessage): boolean {
  return m.kind !== 'state'
}

/**
 * Lays messages (oldest first) out as rows. A day separator opens every day,
 * the unread marker sits after the last read message when someone else wrote
 * after it, and a message continues the group above it when the same person
 * sent it within groupWindowMs on the same day with nothing in between.
 */
export function buildTimeline(messages: readonly ChatMessage[], opts: TimelineOptions): TimelineItem[] {
  const out: TimelineItem[] = []
  let lastDay = ''
  let prev: { sender: string; ts: number; groups: boolean } | null = null

  const markerIndex = opts.unreadAfter ? messages.findIndex((m) => m.id === opts.unreadAfter) : -1
  const unreadFollows = markerIndex >= 0 && messages.slice(markerIndex + 1).some((m) => !m.own && groups(m))

  messages.forEach((m, i) => {
    const day = dayKey(m.ts)
    if (day !== lastDay) {
      out.push({ type: 'day', key: `day-${day}`, ts: m.ts })
      lastDay = day
      prev = null
    }
    const continued =
      prev !== null && prev.groups && groups(m) && prev.sender === m.sender && m.ts - prev.ts <= groupWindowMs
    out.push({ type: 'message', key: m.id, message: m, continued })
    prev = { sender: m.sender, ts: m.ts, groups: groups(m) }
    if (unreadFollows && i === markerIndex) {
      out.push({ type: 'unread', key: 'unread' })
      prev = null
    }
  })

  for (const p of opts.pending ?? []) {
    const day = dayKey(p.createdAt)
    if (day !== lastDay) {
      out.push({ type: 'day', key: `day-${day}`, ts: p.createdAt })
      lastDay = day
      prev = null
    }
    const continued = prev !== null && prev.groups && prev.sender === opts.ownId && p.createdAt - prev.ts <= groupWindowMs
    out.push({ type: 'pending', key: p.localId, localId: p.localId, continued })
    prev = { sender: opts.ownId, ts: p.createdAt, groups: true }
  }
  return out
}
