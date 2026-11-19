// threadrows.ts lays a grouped message list out as rows: one per conversation,
// and beneath each open one the rest of its messages, newest first like the
// list around them.

import type { MessageSummary, ThreadMessage, ThreadSummary } from './types'

/** One row of the message list. */
export interface ListRow {
  /** Stable across renders and distinct between a conversation and its replies. */
  key: string
  message: MessageSummary
  /** The conversation the row stands for, null for a reply row or an ungrouped list. */
  thread: ThreadSummary | null
  /** The id of the conversation row a reply sits under, 0 for any other row. */
  parentId: number
  /** The thread id a reply row belongs to, 0 for any other row. */
  threadId: number
  fromMe: boolean
}

/**
 * sameMessage tells whether a conversation member is the row's own message,
 * which the thread can return as a copy from another folder under another id.
 */
export function sameMessage(a: MessageSummary, b: MessageSummary): boolean {
  return a.id === b.id || (a.date === b.date && a.fromAddress === b.fromAddress && a.subject === b.subject)
}

/**
 * replies is a conversation's messages other than the one its row shows,
 * newest first.
 */
export function replies(members: ThreadMessage[], head: MessageSummary): ThreadMessage[] {
  return members.filter((m) => !sameMessage(m, head)).reverse()
}

/**
 * listRows builds the rows for the loaded heads. threads is undefined for an
 * ungrouped list; expanded holds the open conversations' thread ids and
 * members their loaded messages.
 */
export function listRows(
  heads: MessageSummary[],
  threads: Record<number, ThreadSummary> | undefined,
  expanded: ReadonlySet<number>,
  members: ReadonlyMap<number, ThreadMessage[]>,
): ListRow[] {
  const rows: ListRow[] = []
  for (const head of heads) {
    const thread = threads?.[head.id] ?? null
    rows.push({ key: `m${head.id}`, message: head, thread, parentId: 0, threadId: 0, fromMe: false })
    if (!thread || thread.count < 2 || !expanded.has(thread.threadId)) {
      continue
    }
    for (const reply of replies(members.get(thread.threadId) ?? [], head)) {
      rows.push({
        key: `r${reply.id}`,
        message: reply,
        thread: null,
        parentId: head.id,
        threadId: thread.threadId,
        fromMe: reply.fromMe,
      })
    }
  }
  return rows
}

/**
 * threadAware stands each conversation's newest message in for the whole
 * conversation, for filtering: unread, flagged and attachments if any member
 * is. A list that is not grouped comes back as it is.
 */
export function threadAware(heads: MessageSummary[], threads: Record<number, ThreadSummary> | undefined): MessageSummary[] {
  if (!threads) {
    return heads
  }
  return heads.map((m) => {
    const thread = threads[m.id]
    if (!thread) {
      return m
    }
    return {
      ...m,
      seen: thread.unreadCount === 0,
      flagged: thread.flagged,
      hasAttachments: thread.hasAttachments,
    }
  })
}
