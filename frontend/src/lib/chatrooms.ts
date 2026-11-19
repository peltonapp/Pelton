// chatrooms.ts sorts the chat room list into its sections and words each
// room's preview line. Pure, so the list component stays a view.

import type { ChatRoom } from './types'
import { matchesQuery } from './spacesearch'

/** One heading of the room list and the rooms under it. */
export interface RoomSection {
  key: 'invites' | 'favourites' | 'people' | 'rooms' | 'low'
  labelKey: string
  rooms: ChatRoom[]
}

const order: { key: RoomSection['key']; labelKey: string; test: (r: ChatRoom) => boolean }[] = [
  { key: 'invites', labelKey: 'chat.rooms.invites', test: (r) => r.membership === 'invite' },
  { key: 'favourites', labelKey: 'chat.rooms.favourites', test: (r) => r.favourite },
  { key: 'people', labelKey: 'chat.rooms.people', test: (r) => r.kind === 'direct' && !r.lowPriority },
  { key: 'rooms', labelKey: 'chat.rooms.rooms', test: (r) => !r.lowPriority },
  { key: 'low', labelKey: 'chat.rooms.lowPriority', test: () => true },
]

/**
 * Splits rooms into sections, each room in the first that takes it. Spaces
 * are not rooms to chat in and are left out; a space filter keeps the rooms
 * that space lists, and a query keeps the rooms it matches. Empty sections
 * are dropped. Rooms keep the order they come in, newest first.
 */
export function roomSections(rooms: readonly ChatRoom[], query: string, space: string): RoomSection[] {
  const q = query.trim()
  const sections: RoomSection[] = order.map((o) => ({ key: o.key, labelKey: o.labelKey, rooms: [] }))
  for (const room of rooms) {
    if (room.kind === 'space') {
      continue
    }
    if (space && !room.spaces.includes(space) && room.membership !== 'invite') {
      continue
    }
    if (q && !matchesQuery(q, [room.name, room.alias, room.topic, room.directUser, room.inviterName])) {
      continue
    }
    const i = order.findIndex((o) => o.test(room))
    sections[i].rooms.push(room)
  }
  return sections.filter((s) => s.rooms.length > 0)
}

/** The second line of a room row: who said what last, or what kind of thing it was. */
export function previewLine(room: ChatRoom, t: (key: string) => string): string {
  let text = room.preview
  switch (room.previewKind) {
    case 'encrypted':
      text = t('chat.preview.encrypted')
      break
    case 'redacted':
      text = t('chat.preview.redacted')
      break
    case 'image':
    case 'video':
    case 'audio':
    case 'file':
    case 'sticker':
      text = `${t(`chat.preview.${room.previewKind}`)}: ${room.preview}`
      break
  }
  if (!text) {
    return room.kind === 'direct' ? '' : room.topic
  }
  if (room.previewSender && room.kind !== 'direct') {
    return `${room.previewSender}: ${text}`
  }
  if (!room.previewSender && room.previewKind) {
    return `${t('chat.preview.you')}: ${text}`
  }
  return text
}
