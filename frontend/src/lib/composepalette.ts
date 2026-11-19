// composepalette.ts puts the composer actions into the command palette while a
// composer is open in the window: each runs on the composer that last had
// focus, through stores/composecommands.ts. Inserting a snippet and moving a
// recipient open a step listing what to act on, so the choice is made without
// leaving the palette.

import type { ComponentType } from 'svelte'
import { get } from 'svelte/store'
import {
  IconArrowsExchange,
  IconBellRinging,
  IconChevronsDown,
  IconChevronsUp,
  IconEyeOff,
  IconTextPlus,
  IconUserPlus,
} from '@tabler/icons-svelte'
import type { CommandStep, PaletteCommand } from './commands'
import type { Snippet } from './types'
import { composeActions, type ComposeAction } from './shortcuts'
import { registerPaletteSource } from './palettesources'
import { shortcutLabel } from './i18n'
import { triggerPrefix } from './snippets'
import { fieldBeside, recipientFields } from './recipientmoves'
import { splitAddressList } from './mailcompose'
import { activeComposer, runComposeCommand, type ChipRef } from '../stores/composecommands'
import { composeSessions } from '../stores/compose'
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
  'compose-intro-bcc': { labelKey: 'recipients.introBcc', keywordsKey: 'recipients.keywords', icon: IconEyeOff },
  'compose-toggle-reply-all': { labelKey: 'recipients.toggleReplyAll', keywordsKey: 'recipients.keywords', icon: IconArrowsExchange },
  'compose-cc-sender': { labelKey: 'recipients.ccSender', keywordsKey: 'recipients.keywords', icon: IconUserPlus },
  'compose-recipient-down': { labelKey: 'recipients.moveDown', keywordsKey: 'recipients.keywords', icon: IconChevronsDown },
  'compose-recipient-up': { labelKey: 'recipients.moveUp', keywordsKey: 'recipients.keywords', icon: IconChevronsUp },
}

/** What composeCommands needs from the running window. */
export interface ComposePaletteContext {
  t: (key: string) => string
  /** Whether a composer is open to act on. */
  open: boolean
  /** The current key of an action, formatted, or ''. */
  hint: (action: ComposeAction) => string
  snippets: Snippet[]
  /** The recipients of the composer acted on, in field order. */
  recipients: ChipRef[]
  run: (action: ComposeAction, snippetId?: number, recipient?: ChipRef) => void
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

const fieldKeys: Record<ChipRef['field'], string> = { to: 'compose.field.to', cc: 'compose.field.cc', bcc: 'compose.field.bcc' }

// recipientStep lists the recipients that can move the given way.
function recipientStep(ctx: ComposePaletteContext, action: ComposeAction, dir: 'down' | 'up'): CommandStep {
  return {
    label: ctx.t(composeActionMeta[action].labelKey),
    placeholder: ctx.t('recipients.pick'),
    items: ctx.recipients.flatMap((chip) => {
      const to = fieldBeside(chip.field, dir)
      if (!to) {
        return []
      }
      return [
        {
          id: `recipient:${dir}:${chip.field}:${chip.value}`,
          group: 'action' as const,
          label: chip.value,
          hint: `${ctx.t(fieldKeys[chip.field])} → ${ctx.t(fieldKeys[to])}`,
          icon: composeActionMeta[action].icon,
          run: () => ctx.run(action, undefined, chip),
        },
      ]
    }),
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
      run: () => {
        if (action === 'compose-insert-snippet' && ctx.snippets.length > 0) {
          return snippetStep(ctx)
        }
        if (action === 'compose-recipient-down' || action === 'compose-recipient-up') {
          return recipientStep(ctx, action, action === 'compose-recipient-down' ? 'down' : 'up')
        }
        ctx.run(action)
      },
    }
  })
}

// activeRecipients lists the active composer's recipients, field by field.
function activeRecipients(): ChipRef[] {
  const id = get(activeComposer)
  const session = get(composeSessions).find((s) => s.id === id)
  if (!session) {
    return []
  }
  return recipientFields.flatMap((field) => splitAddressList(session[field]).map((value) => ({ value, field })))
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
      recipients: activeRecipients(),
      run: (action, snippetId, recipient) => void runComposeCommand(action, snippetId, recipient),
    }),
  )
}
