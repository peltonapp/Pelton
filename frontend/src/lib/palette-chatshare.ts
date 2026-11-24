// palette-chatshare.ts puts "Share to chat" into the command palette while a
// message is open and chat is available.

import { get } from 'svelte/store'
import { IconMessageShare } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import { chatAvailable } from './chatavailable'
import type { PaletteCommand } from './commands'
import { messageDetail } from '../stores/message'
import { openShareToChat } from '../stores/chatshare'

registerPaletteSource('chatshare', (t) => {
  const message = get(messageDetail).data
  if (!message || !get(chatAvailable)) {
    return []
  }
  const out: PaletteCommand[] = [
    {
      id: 'chat:share',
      group: 'mail',
      label: t('chat.share.action'),
      keywords: t('chat.share.keywords'),
      icon: IconMessageShare,
      run: () => openShareToChat(message),
    },
  ]
  return out
})
