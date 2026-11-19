// palette-availability.ts puts "Share availability" into the command palette
// while a composer is open in the main window: it opens the availability
// panel of the newest one, which inserts free times into the message.

import { get } from 'svelte/store'
import { IconCalendarTime } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import { availabilityRequest, composeSessions } from '../stores/compose'
import { prefs } from '../stores/prefs'

registerPaletteSource('availability', (t) => {
  const open = get(composeSessions).filter((s) => s.placement !== 'leaving')
  const target = open[open.length - 1]
  if (!target || get(prefs).design === 'classic') {
    return []
  }
  return [
    {
      id: 'compose:share-availability',
      group: 'calendar',
      label: t('avail.open'),
      keywords: t('avail.keywords'),
      icon: IconCalendarTime,
      run: () => availabilityRequest.set(target.id),
    },
  ]
})
