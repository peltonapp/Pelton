// chatstate.ts words the membership and room changes a chat timeline shows as
// small rows: who joined, left or was invited, and what the room was renamed
// to. The backend says what happened; the wording lives here.

import type { ChatMessage } from './types'

/** The sentence for a state row, or empty for one with nothing to say. */
export function stateText(m: ChatMessage, t: (key: string) => string): string {
  const s = m.state
  if (!s) {
    return ''
  }
  const fill = (key: string): string =>
    t(key)
      .replace('{actor}', m.senderName)
      .replace('{target}', s.targetName || s.target)
      .replace('{value}', s.value)
  switch (s.kind) {
    case 'joined':
    case 'left':
    case 'rejected':
    case 'avatar':
    case 'created':
    case 'encryption':
    case 'roomAvatar':
    case 'invited':
    case 'kicked':
    case 'banned':
    case 'unbanned':
    case 'renamed':
      return fill(`chat.state.${s.kind}`)
    case 'name':
      return fill(s.value ? 'chat.state.name' : 'chat.state.nameRemoved')
    case 'topic':
      return fill(s.value ? 'chat.state.topic' : 'chat.state.topicRemoved')
  }
  return ''
}
