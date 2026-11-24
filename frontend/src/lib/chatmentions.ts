// chatmentions.ts finds the person being mentioned as the user types @ and a
// name in the chat composer, and keeps track of who the finished message
// names, so the people mentioned get notified.

import type { ChatMember } from './types'

/** A mention being typed: where its @ is and the name so far. */
export interface MentionQuery {
  start: number
  query: string
}

/** Finds an @mention being typed just before the caret. */
export function mentionQuery(text: string, caret: number): MentionQuery | null {
  const before = text.slice(0, caret)
  const m = before.match(/(^|\s)@([^\s@]{0,40})$/)
  if (!m) {
    return null
  }
  return { start: caret - m[2].length - 1, query: m[2].toLowerCase() }
}

/**
 * Members for a query: names starting with it, then names or ids holding it.
 * The user is never offered.
 */
export function matchMembers(members: readonly ChatMember[], query: string, ownId: string, limit = 6): ChatMember[] {
  const q = query.toLowerCase()
  const starts: ChatMember[] = []
  const contains: ChatMember[] = []
  for (const m of members) {
    if (m.userId === ownId || m.membership === 'leave' || m.membership === 'ban') {
      continue
    }
    const name = m.name.toLowerCase()
    const local = m.userId.slice(1).toLowerCase()
    if (name.startsWith(q) || local.startsWith(q)) {
      starts.push(m)
    } else if (name.includes(q) || local.includes(q)) {
      contains.push(m)
    }
  }
  return [...starts, ...contains].slice(0, limit)
}

/** Puts a chosen member in place of the mention being typed. */
export function applyMention(text: string, query: MentionQuery, caret: number, member: ChatMember): { text: string; caret: number } {
  const insert = `@${member.name} `
  const next = text.slice(0, query.start) + insert + text.slice(caret)
  return { text: next, caret: query.start + insert.length }
}

/** The mentioned people whose @name is still in the text. */
export function mentionsIn(text: string, chosen: readonly { userId: string; name: string }[]): { userId: string; name: string }[] {
  const seen = new Set<string>()
  return chosen.filter((m) => {
    if (seen.has(m.userId) || !text.includes(`@${m.name}`)) {
      return false
    }
    seen.add(m.userId)
    return true
  })
}
