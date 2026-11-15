// palette-sieve.ts puts the out of office and server filter pages of the new
// settings screen into the command palette.

import { IconPlaneDeparture, IconFilter } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import { openSettingsAt } from '../stores/settingsnav'

registerPaletteSource('sieve', (t) => [
  {
    id: 'settings:vacation',
    group: 'setting',
    label: t('settings2.page.vacation'),
    keywords: t('sieve.palette.vacationKeywords'),
    icon: IconPlaneDeparture,
    run: () => openSettingsAt('vacation'),
  },
  {
    id: 'settings:filters',
    group: 'setting',
    label: t('settings2.page.filters'),
    keywords: t('sieve.palette.filtersKeywords'),
    icon: IconFilter,
    run: () => openSettingsAt('filters'),
  },
])
