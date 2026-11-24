// location-notes.ts lets Notes take part in back and forward: the note open
// in the editor, by its path in the vault.

import { get } from 'svelte/store'
import { registerLocationProvider, oneSideEmpty, type NotesLocation } from './locations'
import { activeNote, notesByPath, openNote, vaultIndex } from '../stores/notes'
import { openSpace } from '../stores/space'

/** A note's name from its path: the file name without .md. */
export function noteName(path: string): string {
  const file = path.slice(path.lastIndexOf('/') + 1)
  return file.replace(/\.md$/i, '')
}

registerLocationProvider('notes', {
  watch: [activeNote, notesByPath],
  describe(): NotesLocation {
    const path = get(activeNote)
    return { space: 'notes', note: path ?? undefined }
  },
  label(loc) {
    if (loc.note === undefined) {
      return ''
    }
    return get(notesByPath).get(loc.note)?.title || noteName(loc.note)
  },
  async restore(loc) {
    if (loc.note !== undefined) {
      // with the vault index loaded, a note that is not in it has gone.
      if (get(vaultIndex) !== null && !get(notesByPath).has(loc.note)) {
        return false
      }
      openNote(loc.note)
    }
    openSpace('notes')
    return true
  },
  refines(from, to) {
    return oneSideEmpty(from.note, to.note)
  },
})
