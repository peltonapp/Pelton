// composepalette.ts puts the composer actions into the command palette while a
// composer is open in the window: each runs on the composer that last had
// focus, through stores/composecommands.ts. Inserting a snippet opens a step
// listing the snippets, so one is picked without leaving the palette.

import type { ComponentType } from 'svelte'
import { get } from 'svelte/store'
import { IconBellRinging, IconTextPlus } from '@tabler/icons-svelte'
import type { CommandStep, PaletteCommand } from './commands'
import type { Snippet } from './types'
import { composeActions, type ComposeAction } from './shortcuts'
import { registerPaletteSource } from './palettesources'
import { shortcutLabel } from './i18n'
import { triggerPrefix } from './snippets'
import { activeComposer, runComposeCommand } from '../stores/composecommands'
import { bindings } from '../stores/shortcuts'
import { snippets } from '../stores/snippets'

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
  'compose-insert-snippet': { labelKey: 'snippets.insert', keywordsKey: 'snippets.keywords', icon: IconTextPlus },
}

/** What composeCommands needs from the running window. */
export interface ComposePaletteContext {
  t: (key: string) => string
  /** Whether a composer is open to act on. */
  open: boolean
  /** The current key of an action, formatted, or ''. */
  hint: (action: ComposeAction) => string
  snippets: Snippet[]
  run: (action: ComposeAction, snippetId?: number) => void
}

// snippetStep lists the snippets to insert.
function snippetStep(ctx: ComposePaletteContext): CommandStep {
  return {
    label: ctx.t('snippets.insert'),
    placeholder: ctx.t('snippets.search'),
    items: ctx.snippets.map((s) => ({
      id: `snippet:${s.id}`,
      group: 'action' as const,
      label: s.name,
      keywords: s.body.slice(0, 120),
      hint: s.trigger ? `${triggerPrefix}${s.trigger}` : undefined,
      icon: IconTextPlus,
      run: () => ctx.run('compose-insert-snippet', s.id),
    })),
  }
}

/** composeCommands lists the composer actions as palette entries, or none when no composer is open. */
export function composeCommands(ctx: ComposePaletteContext): PaletteCommand[] {
  if (!ctx.open) {
    return []
  }
  return composeActions.map((action) => {
    const meta = composeActionMeta[action]
    return {
      id: `compose:${action}`,
      group: 'action' as const,
      label: ctx.t(meta.labelKey),
      keywords: meta.keywordsKey ? ctx.t(meta.keywordsKey) : undefined,
      hint: ctx.hint(action),
      icon: meta.icon,
      run: () => (action === 'compose-insert-snippet' && ctx.snippets.length > 0 ? snippetStep(ctx) : ctx.run(action)),
    }
  })
}

/** registerComposePalette adds the composer actions to this window's palette. */
export function registerComposePalette(): void {
  registerPaletteSource('compose', (t) =>
    composeCommands({
      t,
      open: get(activeComposer) !== null,
      hint: (action) => {
        const combo = (get(bindings) as Record<string, string>)[action] ?? ''
        return combo ? shortcutLabel(combo) : ''
      },
      snippets: get(snippets),
      run: (action, snippetId) => void runComposeCommand(action, snippetId),
    }),
  )
}
