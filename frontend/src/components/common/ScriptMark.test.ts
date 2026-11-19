import { afterEach, describe, expect, it } from 'vitest'
import { render, screen, within } from '@testing-library/svelte'
import { tick } from 'svelte'
import { IconArchive, IconPlayerPlay } from '@tabler/icons-svelte'
import ContextMenu from './ContextMenu.svelte'
import CommandPalette from './CommandPalette.svelte'
import { closeContextMenu, openContextMenu } from '../../stores/contextmenu'
import { closePalette, paletteOpen, paletteQuery } from '../../stores/palette'
import { placedScripts } from '../../stores/scripts'
import { prefs } from '../../stores/prefs'
import { extraPaletteCommands } from '../../lib/palettesources'
import { scriptEntryName, scriptText } from '../../lib/scriptmark'
import '../../lib/scriptplacements'
import '../../lib/palette-automations'
import type { PaletteCommand } from '../../lib/commands'

// the palette fades in, and jsdom has no web animations. A finished stub lets svelte run it to its end.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

const say = (key: string): string => (key === 'scripts.mark.name' ? 'Script: {name}' : key)

afterEach(() => {
  closeContextMenu()
  closePalette()
  placedScripts.set({})
  prefs.update((p) => ({ ...p, design: 'new' }))
})

describe('scriptText and scriptEntryName', () => {
  it('take every lookalike of the mark out', () => {
    expect(scriptText('⚡️ Go ☇ now ↯ϟ\u{1F5F2}')).toBe('Go now')
  })

  it('name the script beside the label', () => {
    expect(scriptEntryName('Send', 'CRM', say)).toBe('Send, Script: CRM')
    expect(scriptEntryName('CRM', 'CRM', say)).toBe('Script: CRM')
    expect(scriptEntryName('⚡', '⚡ CRM', say)).toBe('Script: CRM')
  })
})

describe.each(['new', 'classic'] as const)('context menu scripts in the %s design', (design) => {
  it('sit in their own Scripts group, each with the mark and its script named', async () => {
    prefs.update((p) => ({ ...p, design }))
    placedScripts.set({ 'mail.row': [{ scriptId: 4, name: '⚡ Tag invoices', description: '', options: {} }] })
    render(ContextMenu)
    openContextMenu(0, 0, [{ label: 'Archive', icon: IconArchive, action: () => {} }], { place: 'mail.row', items: [{ id: '7' }] })
    await tick()
    const group = screen.getByRole('group', { name: 'Scripts' })
    expect(within(group).getByText('Scripts')).toBeInTheDocument()
    const item = within(group).getByRole('menuitem', { name: 'Script: Tag invoices' })
    expect(item).toHaveAttribute('title', 'Script: Tag invoices')
    expect(item.querySelector('[data-script-mark]')).not.toBeNull()
    expect(item.textContent).not.toContain('⚡')
    expect(screen.getByRole('menuitem', { name: 'Archive' }).querySelector('[data-script-mark]')).toBeNull()
  })
})

describe('command palette scripts', () => {
  it('carry the mark and name the script', async () => {
    placedScripts.set({ palette: [{ scriptId: 9, name: 'Weekly digest', description: '', options: {} }] })
    const fromSource = extraPaletteCommands((key) => (key === 'palette.automations.run' ? 'Run {name}' : key))
    const run = fromSource.find((c) => c.id === 'automations:run:9')
    expect(run).toMatchObject({ label: 'Run Weekly digest', script: 'Weekly digest' })
    expect(fromSource.filter((c) => c.script !== undefined)).toHaveLength(1)

    const plain: PaletteCommand = { id: 'x', group: 'action', label: 'Archive', icon: IconPlayerPlay, run: () => {} }
    render(CommandPalette, { commands: [run!, plain] })
    paletteOpen.set(true)
    paletteQuery.set('e')
    await tick()
    const option = screen.getByRole('option', { name: 'Run Weekly digest, Script: Weekly digest' })
    expect(option.querySelector('[data-script-mark]')).not.toBeNull()
    expect(screen.getByRole('option', { name: /^Archive/ }).querySelector('[data-script-mark]')).toBeNull()
    paletteQuery.set('')
  })
})
