// location-chat.ts lets Chat take part in back and forward: the room open in
// the space. with chat switched off, a chat place is gone.

import { get } from 'svelte/store'
import { registerLocationProvider, oneSideEmpty, type ChatLocation } from './locations'
import { activeRoomId, chatRooms, closeRoom, openRoom } from '../stores/chat'
import { prefs } from '../stores/prefs'
import { openSpace } from '../stores/space'

registerLocationProvider('chat', {
  watch: [activeRoomId, chatRooms],
  describe(): ChatLocation {
    const room = get(activeRoomId)
    return { space: 'chat', room: room ?? undefined }
  },
  label(loc) {
    return loc.room === undefined ? '' : (get(chatRooms).find((r) => r.id === loc.room)?.name ?? '')
  },
  async restore(loc) {
    if (!get(prefs).chatEnabled) {
      return false
    }
    const rooms = get(chatRooms)
    if (loc.room !== undefined && rooms.length > 0 && !rooms.some((r) => r.id === loc.room)) {
      return false
    }
    openSpace('chat')
    if (loc.room === undefined) {
      closeRoom()
    } else {
      await openRoom(loc.room)
    }
    return true
  },
  refines(from, to) {
    return oneSideEmpty(from.room, to.room)
  },
})
