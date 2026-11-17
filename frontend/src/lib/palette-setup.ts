// palette-setup.ts puts running the setup again into the command palette of
// the new design.

import { IconRoute } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import { requestSetup } from '../stores/setuprun'

registerPaletteSource('setup', (t) => [
  { id: 'setup:run-again', group: 'action', label: t('setup.runAgain'), keywords: t('setup.palette.keywords'), icon: IconRoute, run: () => requestSetup() },
])
