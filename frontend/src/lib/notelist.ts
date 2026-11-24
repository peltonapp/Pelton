// notelist.ts decides what the note list shows: the notes of a folder, a tag,
// a project or the starred ones, in the chosen order, and the folder tree the
// sidebar draws. Everything is pure, so the screen and the tests agree.

import { inProject } from './noteprojects'
import type { VaultNote } from './types'

/** What the note list is showing. A project is named by its uid and name, as front matter names it. */
export type NoteScope =
  | { kind: 'all' }
  | { kind: 'folder'; path: string }
  | { kind: 'tag'; tag: string }
  | { kind: 'starred' }
  | { kind: 'project'; uid: string; name: string }

/** How the note list is ordered. */
export type NoteSort = 'modified' | 'modifiedOld' | 'title' | 'titleDesc'

/** The sort orders, in menu order. */
export const noteSorts: NoteSort[] = ['modified', 'modifiedOld', 'title', 'titleDesc']

/** True when a note carries tag or a tag nested under it, ignoring case. */
export function hasTag(note: Pick<VaultNote, 'tags'>, tag: string): boolean {
  const want = tag.toLowerCase()
  return note.tags.some((t) => {
    const l = t.toLowerCase()
    return l === want || l.startsWith(`${want}/`)
  })
}

/** The notes a scope holds. A folder holds its notes and those of its subfolders. */
export function notesInScope(notes: readonly VaultNote[], scope: NoteScope, starred: readonly string[] = []): VaultNote[] {
  switch (scope.kind) {
    case 'all':
      return [...notes]
    case 'folder':
      return scope.path ? notes.filter((n) => n.path.startsWith(`${scope.path}/`)) : [...notes]
    case 'tag':
      return notes.filter((n) => hasTag(n, scope.tag))
    case 'project':
      return notes.filter((n) => inProject(n.props, scope))
    case 'starred': {
      const order = new Map(starred.map((p, i) => [p, i]))
      return notes.filter((n) => order.has(n.path)).sort((a, b) => order.get(a.path)! - order.get(b.path)!)
    }
  }
}

/** Orders notes; equal keys fall back to the path so the order is stable. */
export function sortNotes(notes: readonly VaultNote[], sort: NoteSort): VaultNote[] {
  const byTitle = (a: VaultNote, b: VaultNote): number => a.title.localeCompare(b.title, undefined, { numeric: true, sensitivity: 'base' }) || a.path.localeCompare(b.path)
  const out = [...notes]
  switch (sort) {
    case 'modified':
      return out.sort((a, b) => b.mtime - a.mtime || a.path.localeCompare(b.path))
    case 'modifiedOld':
      return out.sort((a, b) => a.mtime - b.mtime || a.path.localeCompare(b.path))
    case 'title':
      return out.sort(byTitle)
    case 'titleDesc':
      return out.sort((a, b) => byTitle(b, a))
  }
}

/** One folder of the sidebar tree, with its depth for indentation. */
export interface FolderRow {
  path: string
  name: string
  depth: number
  hasChildren: boolean
}

/**
 * The folder tree as the rows the sidebar draws, sorted by name, skipping
 * the children of folders that are not expanded.
 */
export function folderRows(folders: readonly string[], expanded: ReadonlySet<string>): FolderRow[] {
  const sorted = [...folders].sort((a, b) => a.localeCompare(b, undefined, { numeric: true, sensitivity: 'base' }))
  const all = new Set(sorted)
  const out: FolderRow[] = []
  for (const path of sorted) {
    const parts = path.split('/')
    let visible = true
    for (let i = 1; i < parts.length; i++) {
      const parent = parts.slice(0, i).join('/')
      if (!all.has(parent) || !expanded.has(parent)) {
        visible = false
        break
      }
    }
    if (!visible) {
      continue
    }
    out.push({ path, name: parts[parts.length - 1], depth: parts.length - 1, hasChildren: sorted.some((f) => f.startsWith(`${path}/`)) })
  }
  return out
}

/** How many notes each folder holds, subfolders included. */
export function folderCounts(notes: readonly VaultNote[]): Map<string, number> {
  const out = new Map<string, number>()
  for (const n of notes) {
    const parts = n.path.split('/')
    for (let i = 1; i < parts.length; i++) {
      const f = parts.slice(0, i).join('/')
      out.set(f, (out.get(f) ?? 0) + 1)
    }
  }
  return out
}
