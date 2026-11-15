// palette-attachments.ts puts the attachments browser and the reader's sender
// panel into the command palette of the new design.

import { get } from 'svelte/store'
import { IconPaperclip, IconLayoutSidebarRight } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import { shortcutLabel } from './i18n'
import { bindings } from '../stores/shortcuts'
import { prefs, setSenderPanel } from '../stores/prefs'
import { openAttachments } from '../stores/attachments'
import { openSpace } from '../stores/space'

function hint(action: string): string {
  const combo = (get(bindings) as Record<string, string>)[action] ?? ''
  return combo ? shortcutLabel(combo) : ''
}

registerPaletteSource('attachments', (t) => {
  const panelOn = get(prefs).senderPanel
  return [
    {
      id: 'attachments:open',
      group: 'navigate',
      label: t('attachments.palette.open'),
      keywords: t('attachments.palette.keywords'),
      hint: hint('open-attachments'),
      icon: IconPaperclip,
      run: () => {
        openSpace('mail')
        openAttachments()
      },
    },
    {
      id: 'attachments:sender-panel',
      group: 'action',
      label: panelOn ? t('senderPanel.hide') : t('senderPanel.show'),
      keywords: t('senderPanel.keywords'),
      hint: hint('toggle-sender-panel'),
      icon: IconLayoutSidebarRight,
      run: () => setSenderPanel(!panelOn),
    },
  ]
})
