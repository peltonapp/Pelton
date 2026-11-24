// palette-setup.ts puts What's new and running the setup again into the
// command palette of the new design.

import { IconSparkles, IconRoute } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import { openWhatsNew } from '../stores/whatsnew'
import { requestSetup } from '../stores/setuprun'

registerPaletteSource('setup', (t) => [
  { id: 'setup:whats-new', group: 'navigate', label: t('whatsnew.title'), keywords: t('whatsnew.palette.keywords'), icon: IconSparkles, run: () => openWhatsNew() },
  { id: 'setup:run-again', group: 'action', label: t('setup.runAgain'), keywords: t('setup.palette.keywords'), icon: IconRoute, run: () => requestSetup() },
])
