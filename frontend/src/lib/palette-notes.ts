// palette-notes.ts puts the Notes space into the command palette: going to
// Notes, a new note, today's daily note, the quick switcher, a new note from
// a template, the graph, opening a note by name and the notes settings. The
// entries work from any space; each one shows Notes first.

import { get } from 'svelte/store'
import { IconAffiliate, IconFilePlus, IconNote, IconNotebook, IconSearch, IconSettings, IconSun, IconTemplate } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import type { CommandStep, PaletteCommand } from './commands'
import { openSpace } from '../stores/space'
import { openSettingsAt } from '../stores/settingsnav'
import { menuHint } from '../stores/shortcuts'
import { openNote, requestNotes, vaultNotes } from '../stores/notes'

// a step listing the notes; picking one opens it.
function notesStep(t: (key: string) => string): CommandStep {
  return {
    label: t('palette.notes.find'),
    placeholder: t('palette.notes.findPlaceholder'),
    items: get(vaultNotes).map((n) => ({
      id: `notes:show:${n.path}`,
      group: 'notes' as const,
      label: n.title,
      keywords: [n.path, ...n.aliases].join(' '),
      icon: IconNote,
      run: () => {
        openNote(n.path)
        openSpace('notes')
      },
    })),
  }
}

registerPaletteSource('notes', (t) => {
  const out: PaletteCommand[] = [
    { id: 'notes:open', group: 'notes', label: t('palette.notes.open'), hint: menuHint('go-notes'), icon: IconNotebook, run: () => openSpace('notes') },
    { id: 'notes:new', group: 'notes', label: t('notes.newNote'), keywords: t('palette.notes.newKeywords'), hint: menuHint('notes-new'), icon: IconFilePlus, run: () => requestNotes({ kind: 'new' }) },
    { id: 'notes:today', group: 'notes', label: t('shortcut.notesToday'), keywords: t('palette.notes.todayKeywords'), hint: menuHint('notes-today'), icon: IconSun, run: () => requestNotes({ kind: 'today' }) },
    { id: 'notes:switcher', group: 'notes', label: t('shortcut.notesSwitcher'), hint: menuHint('notes-switcher'), icon: IconSearch, run: () => requestNotes({ kind: 'switcher' }) },
    { id: 'notes:template', group: 'notes', label: t('notes.fromTemplate'), icon: IconTemplate, run: () => requestNotes({ kind: 'template' }) },
    { id: 'notes:graph', group: 'notes', label: t('shortcut.notesGraph'), hint: menuHint('notes-graph'), icon: IconAffiliate, run: () => requestNotes({ kind: 'graph' }) },
    { id: 'notes:settings', group: 'notes', label: t('palette.notes.settings'), icon: IconSettings, run: () => openSettingsAt('notes') },
  ]
  if (get(vaultNotes).length) {
    out.push({ id: 'notes:find', group: 'notes', label: t('palette.notes.find'), icon: IconNote, run: () => notesStep(t) })
  }
  return out
})
