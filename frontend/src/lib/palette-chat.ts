// palette-chat.ts puts chat into the command palette while chat is on: going
// to Chat, starting a chat or joining a room, opening a room by name,
// verifying this device and the chat settings. With chat off it offers
// nothing.

import { get } from 'svelte/store'
import { IconMessages, IconMessagePlus, IconMessage, IconShieldCheck, IconSettings } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import type { CommandStep, PaletteCommand } from './commands'
import { openSpace, runInSpace } from '../stores/space'
import { openSettingsAt } from '../stores/settingsnav'
import { menuHint } from '../stores/shortcuts'
import { prefs } from '../stores/prefs'
import { chatRooms, chatStatus, openRoom } from '../stores/chat'
import { chatStartVerification } from './api'

// a step listing the joined rooms; picking one opens it.
function roomsStep(t: (key: string) => string): CommandStep {
  return {
    label: t('palette.chat.find'),
    placeholder: t('palette.chat.findPlaceholder'),
    items: get(chatRooms)
      .filter((r) => r.membership === 'join' && r.kind !== 'space')
      .map((r) => ({
        id: `chat:room:${r.id}`,
        group: 'chat' as const,
        label: r.name || r.alias || r.id,
        icon: IconMessage,
        run: () => {
          openSpace('chat')
          void openRoom(r.id)
        },
      })),
  }
}

registerPaletteSource('chat', (t) => {
  if (!get(prefs).chatEnabled || get(prefs).design === 'classic') {
    return []
  }
  const status = get(chatStatus)
  const out: PaletteCommand[] = [
    {
      id: 'chat:open',
      group: 'chat',
      label: t('palette.chat.open'),
      hint: menuHint('go-chat'),
      icon: IconMessages,
      run: () => openSpace('chat'),
    },
    {
      id: 'chat:settings',
      group: 'chat',
      label: t('palette.chat.settings'),
      icon: IconSettings,
      run: () => openSettingsAt('chat'),
    },
  ]
  if (status.state === 'signedOut') {
    return out
  }
  out.push({
    id: 'chat:new',
    group: 'chat',
    label: t('chat.new.title'),
    keywords: t('palette.chat.newKeywords'),
    icon: IconMessagePlus,
    run: () => runInSpace('chat', 'compose'),
  })
  if (get(chatRooms).some((r) => r.membership === 'join')) {
    out.push({
      id: 'chat:find',
      group: 'chat',
      label: t('palette.chat.find'),
      icon: IconMessage,
      run: () => roomsStep(t),
    })
  }
  if (status.encryption.ready && !status.encryption.deviceVerified) {
    out.push({
      id: 'chat:verify',
      group: 'chat',
      label: t('chat.verify.thisDevice'),
      icon: IconShieldCheck,
      run: () => void chatStartVerification(''),
    })
  }
  return out
})
