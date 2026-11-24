// palette-focus.ts puts focus mode and the shortcut sheet into the command
// palette of the new design.

import { get } from 'svelte/store'
import { IconMoon, IconSun, IconClock, IconKeyboard } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import { shortcutLabel } from './i18n'
import { focusEnd } from './focus'
import { bindings } from '../stores/shortcuts'
import { focus, focusMenuOpen, beginFocus, finishFocus } from '../stores/focus'
import { toggleSheet } from '../stores/coach'

function hint(action: string): string {
  const combo = (get(bindings) as Record<string, string>)[action] ?? ''
  return combo ? shortcutLabel(combo) : ''
}

registerPaletteSource('focus', (t) => {
  const keywords = t('focus.palette.keywords')
  const sheet = {
    id: 'coach:sheet',
    group: 'navigate' as const,
    label: t('coach.sheet.title'),
    keywords: t('coach.palette.keywords'),
    hint: hint('shortcut-sheet'),
    icon: IconKeyboard,
    run: () => toggleSheet(),
  }
  if (get(focus).active) {
    return [
      { id: 'focus:end', group: 'action', label: t('focus.end'), keywords, hint: hint('focus-mode'), icon: IconSun, run: () => void finishFocus() },
      sheet,
    ]
  }
  return [
    { id: 'focus:25m', group: 'action', label: t('focus.palette.25m'), keywords, icon: IconMoon, run: () => void beginFocus(focusEnd('25m')) },
    { id: 'focus:1h', group: 'action', label: t('focus.palette.1h'), keywords, icon: IconMoon, run: () => void beginFocus(focusEnd('1h')) },
    { id: 'focus:off', group: 'action', label: t('focus.palette.off'), keywords, icon: IconMoon, run: () => void beginFocus(focusEnd('off')) },
    {
      id: 'focus:until',
      group: 'action',
      label: t('focus.palette.until'),
      keywords,
      hint: hint('focus-mode'),
      icon: IconClock,
      run: () => focusMenuOpen.set(true),
    },
    sheet,
  ]
})
