// palette-automations.ts puts the Automations space into the command palette:
// going there, a new script, importing one, and running each script placed in
// the palette. A script that runs on messages gets the selected ones, or the
// message on screen.

import { IconBolt, IconPlayerPlay, IconPlus, IconUpload } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import type { PaletteCommand } from './commands'
import { currentMessageItems, placedNow, runAt } from './scriptplacements'
import { openSpace, runInSpace } from '../stores/space'
import { menuHint } from '../stores/shortcuts'
import { pickImport } from '../stores/scripts'
import { errorMessage, toastError } from '../stores/toast'

/** The placement of the command palette. */
const palettePlacement = 'palette'

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
  for (const s of placedNow(palettePlacement)) {
    out.push({
      id: `automations:run:${s.scriptId}`,
      group: 'automations',
      label: t('palette.automations.run').replace('{name}', s.name),
      keywords: s.description,
      icon: IconPlayerPlay,
      run: () => runAt(palettePlacement, s.scriptId, currentMessageItems()),
    })
  }
  return out
})
