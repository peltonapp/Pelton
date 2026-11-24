// palette-history.ts puts undo, redo and the recent actions list into the
// command palette of the new design.

import { get } from 'svelte/store'
import { IconArrowBackUp, IconArrowForwardUp, IconHistory } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import type { PaletteCommand } from './commands'
import { prefs } from '../stores/prefs'
import { shortcutLabel } from './i18n'
import { bindings } from '../stores/shortcuts'
import { historyOpen, redoOrSay, undoOrSay } from '../stores/undohistory'

function hint(action: string): string {
  const combo = (get(bindings) as Record<string, string>)[action] ?? ''
  return combo ? shortcutLabel(combo) : ''
}

registerPaletteSource('history', (t) => {
  const keywords = t('history.palette.keywords')
  const out: PaletteCommand[] = [
    { id: 'history:undo', group: 'action', label: t('history.palette.undo'), keywords, hint: hint('undo'), icon: IconArrowBackUp, run: () => void undoOrSay() },
    { id: 'history:redo', group: 'action', label: t('history.palette.redo'), keywords, hint: hint('redo'), icon: IconArrowForwardUp, run: () => void redoOrSay() },
  ]
  // the list lives in the new design's top bar.
  if (get(prefs).design !== 'classic') {
    out.push({ id: 'history:open', group: 'action', label: t('history.palette.open'), keywords, hint: hint('undo-history'), icon: IconHistory, run: () => historyOpen.set(true) })
  }
  return out
})
