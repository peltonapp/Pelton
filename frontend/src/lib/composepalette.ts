// composepalette.ts puts the composer actions into the command palette while a
// composer is open in the window: each runs on the composer that last had
// focus, through stores/composecommands.ts.

import type { ComponentType } from 'svelte'
import { get } from 'svelte/store'
import { IconBellRinging } from '@tabler/icons-svelte'
import type { PaletteCommand } from './commands'
import { composeActions, type ComposeAction } from './shortcuts'
import { registerPaletteSource } from './palettesources'
import { shortcutLabel } from './i18n'
import { activeComposer, runComposeCommand } from '../stores/composecommands'
import { bindings } from '../stores/shortcuts'

/** How a composer action shows in the palette. */
export interface ComposeActionMeta {
  labelKey: string
  /** Localization key of extra words the palette also matches. */
  keywordsKey?: string
  icon: ComponentType
}

/** The palette label and icon of each composer action. */
export const composeActionMeta: Record<ComposeAction, ComposeActionMeta> = {
  'compose-follow-up': { labelKey: 'followUp.menu', keywordsKey: 'followUp.keywords', icon: IconBellRinging },
}

/**
 * composeCommands lists the composer actions as palette entries, or none when
 * no composer is open. hint resolves an action's current key.
 */
export function composeCommands(
  t: (key: string) => string,
  open: boolean,
  hint: (action: ComposeAction) => string,
  run: (action: ComposeAction) => void,
): PaletteCommand[] {
  if (!open) {
    return []
  }
  return composeActions.map((action) => {
    const meta = composeActionMeta[action]
    return {
      id: `compose:${action}`,
      group: 'action' as const,
      label: t(meta.labelKey),
      keywords: meta.keywordsKey ? t(meta.keywordsKey) : undefined,
      hint: hint(action),
      icon: meta.icon,
      run: () => run(action),
    }
  })
}

/** registerComposePalette adds the composer actions to this window's palette. */
export function registerComposePalette(): void {
  registerPaletteSource('compose', (t) =>
    composeCommands(
      t,
      get(activeComposer) !== null,
      (action) => {
        const combo = (get(bindings) as Record<string, string>)[action] ?? ''
        return combo ? shortcutLabel(combo) : ''
      },
      (action) => void runComposeCommand(action),
    ),
  )
}
