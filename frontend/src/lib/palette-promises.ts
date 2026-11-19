// palette-promises.ts puts promises into the command palette: reading the
// open mail for promises, and going to the "I owe" and "Owed to me" lists.
// the entries show in the new design while promises are switched on.

import { get } from 'svelte/store'
import { IconChecklist, IconSearch } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import type { PaletteCommand } from './commands'
import { openSpace } from '../stores/space'
import { prefs } from '../stores/prefs'
import { visibleMessageId } from '../stores/tabs'
import { findPromisesIn, promiseSettings } from '../stores/promises'
import { taskScopeRequest } from '../stores/tasks'

registerPaletteSource('promises', (t) => {
  if (get(prefs).design !== 'new' || !get(promiseSettings).enabled) {
    return []
  }
  const out: PaletteCommand[] = []
  const open = get(visibleMessageId)
  if (open !== null) {
    out.push({
      id: 'promises:find',
      group: 'mail',
      label: t('promises.find'),
      keywords: t('promises.find.keywords'),
      icon: IconSearch,
      run: () => void findPromisesIn(open),
    })
  }
  for (const key of ['iowe', 'owedtome'] as const) {
    out.push({
      id: `promises:${key}`,
      group: 'tasks',
      label: t(`tasks.smart.${key}`),
      icon: IconChecklist,
      run: () => {
        taskScopeRequest.set({ kind: 'smart', key })
        openSpace('tasks')
      },
    })
  }
  return out
})
