// palette-navhistory.ts puts back, forward and the recent places into the
// command palette of the new design.

import { get } from 'svelte/store'
import { IconArrowLeft, IconArrowRight, IconHistory } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import type { PaletteCommand } from './commands'
import { prefs } from '../stores/prefs'
import { menuHint } from '../stores/shortcuts'
import { backVisits, canGoBack, canGoForward, forwardVisits, goBack, goForward, goToVisit, visitLabel } from '../stores/navhistory'

registerPaletteSource('navhistory', (t) => {
  if (get(prefs).design === 'classic') {
    return []
  }
  const keywords = t('navHistory.palette.keywords')
  const out: PaletteCommand[] = []
  if (get(canGoBack)) {
    out.push({ id: 'nav:back', group: 'navigate', label: t('navHistory.back'), keywords, hint: menuHint('nav-back'), icon: IconArrowLeft, run: () => void goBack() })
  }
  if (get(canGoForward)) {
    out.push({ id: 'nav:forward', group: 'navigate', label: t('navHistory.forward'), keywords, hint: menuHint('nav-forward'), icon: IconArrowRight, run: () => void goForward() })
  }
  const behind = get(backVisits)
  const ahead = get(forwardVisits)
  if (behind.length + ahead.length > 0) {
    out.push({
      id: 'nav:recent',
      group: 'navigate',
      label: t('navHistory.palette.recent'),
      keywords,
      icon: IconHistory,
      run: () => ({
        label: t('navHistory.palette.recent'),
        placeholder: t('navHistory.palette.recentPlaceholder'),
        items: [
          ...behind.map(({ visit, index }) => ({ visit, index, hint: '' })),
          ...ahead.map(({ visit, index }) => ({ visit, index, hint: t('navHistory.ahead') })),
        ].map(({ visit, index, hint }) => ({
          id: `nav:visit:${index}:${visit.key}`,
          group: 'navigate' as const,
          label: visitLabel(visit, t),
          hint,
          icon: IconHistory,
          run: () => void goToVisit(index),
        })),
      }),
    })
  }
  return out
})
