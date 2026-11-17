// palette-automations.ts puts the Automations space into the command palette:
// going there, a new script, importing one, and running each switched on
// script that is started by hand. A script that runs on a message gets the
// message on screen.

import { get } from 'svelte/store'
import { IconBolt, IconPlayerPlay, IconPlus, IconUpload } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import type { PaletteCommand } from './commands'
import { openSpace, runInSpace } from '../stores/space'
import { menuHint } from '../stores/shortcuts'
import { pickImport, runNow, scripts } from '../stores/scripts'
import { visibleMessageId } from '../stores/tabs'
import { errorMessage, toastError } from '../stores/toast'

registerPaletteSource('automations', (t) => {
  const out: PaletteCommand[] = [
    {
      id: 'automations:open',
      group: 'automations',
      label: t('palette.automations.open'),
      hint: menuHint('go-automations'),
      icon: IconBolt,
      run: () => openSpace('automations'),
    },
    {
      id: 'automations:new',
      group: 'automations',
      label: t('scripts.new'),
      icon: IconPlus,
      run: () => runInSpace('automations', 'compose'),
    },
    {
      id: 'automations:import',
      group: 'automations',
      label: t('scripts.import'),
      keywords: 'peltonscript',
      icon: IconUpload,
      run: () => void pickImport().catch((err) => toastError(errorMessage(err))),
    },
  ]
  for (const s of get(scripts)) {
    if (!s.enabled || s.trigger.type !== 'manual') {
      continue
    }
    out.push({
      id: `automations:run:${s.id}`,
      group: 'automations',
      label: t('palette.automations.run').replace('{name}', s.name),
      keywords: s.description,
      icon: IconPlayerPlay,
      run: () => {
        const onMessage = s.trigger.params.scope === 'message'
        void runNow(s, onMessage ? (get(visibleMessageId) ?? 0) : 0)
      },
    })
  }
  return out
})
