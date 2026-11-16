// noteeditorcontext.ts builds what the note editor needs from the notes
// space: how links resolve, how embedded files and notes are read, and what
// [[, [[Note# and #tag complete to. The Notes space and a note embedded in
// another space, such as a project's notes, build it the same way from the
// index of the notes folder; only following a link differs.

import { vaultFileData, vaultRead, vaultSaveAttachment } from './api'
import { fuzzyMatch } from './fuzzy'
import { isImagePath, noteFolder } from './notemd'
import { linkText, resolveLink, resolveMarkdownLink, type Resolver } from './noteresolve'
import type { VaultHeading, VaultNote } from './types'
import type { CompletionItem, FollowedLink, NoteContext } from '../components/notes/editor/context'

/** The index of the notes folder, as the editor reads it. */
export interface NoteIndexView {
  notes: readonly VaultNote[]
  /** Every file that is not a note. */
  files: readonly string[]
  resolver: Resolver
  tags: readonly { tag: string; count: number }[]
  byPath: ReadonlyMap<string, VaultNote>
}

/** How the editor's context behaves around the open note. */
export interface NoteEditorOptions {
  /** The vault path of the note being edited, or null. */
  active: string | null
  /** The open note's headings as they are now, which may be ahead of the index. */
  headings: () => readonly Pick<VaultHeading, 'level' | 'text'>[]
  /** Follows a link out of the note. */
  follow: (link: FollowedLink, newTab: boolean) => void
  /** Translates a key. */
  t: (key: string) => string
}

// notes, aliases and images that complete after "[[", the best first.
function completeNotes(view: NoteIndexView, active: string | null, query: string, createLabel: string): CompletionItem[] {
  const text = query.trim()
  const r = view.resolver
  const items: { item: CompletionItem; score: number }[] = []
  for (const n of view.notes) {
    if (n.path === active) {
      continue
    }
    const name = linkText(r, n.path)
    if (!text) {
      items.push({ item: { label: n.title, detail: noteFolder(n.path), insert: name }, score: n.mtime })
      continue
    }
    const s = fuzzyMatch(text, n.title)?.score
    if (s !== undefined) {
      items.push({ item: { label: n.title, detail: noteFolder(n.path), insert: name }, score: s * 1e13 })
    }
    for (const a of n.aliases) {
      const sa = fuzzyMatch(text, a)?.score
      if (sa !== undefined) {
        items.push({ item: { label: a, detail: n.title, insert: `${name}|${a}` }, score: sa * 1e13 - 1 })
      }
    }
  }
  for (const f of view.files) {
    if (!isImagePath(f)) {
      continue
    }
    const base = f.split('/').pop() ?? f
    const s = text ? fuzzyMatch(text, base)?.score : 0
    if (s !== undefined) {
      items.push({ item: { label: base, detail: noteFolder(f), insert: linkText(r, f) }, score: text ? s * 1e13 - 2 : -1 })
    }
  }
  const out = items
    .sort((a, b) => b.score - a.score)
    .slice(0, 12)
    .map((x) => x.item)
  if (text && !view.notes.some((n) => n.title.toLowerCase() === text.toLowerCase())) {
    out.push({ label: text, detail: createLabel, insert: text, create: true })
  }
  return out
}

/** Builds the editor's view of the notes folder for one open note. */
export function noteEditorContext(view: NoteIndexView, opts: NoteEditorOptions): Omit<NoteContext, 'path'> {
  const from = opts.active ?? ''
  return {
    resolve: (target, markdown) => (markdown ? resolveMarkdownLink(view.resolver, target, from) : resolveLink(view.resolver, target, from)),
    fileData: (path) => vaultFileData(path),
    readNote: (path) => vaultRead(path).then((f) => f.content),
    follow: opts.follow,
    completeNotes: (q) => completeNotes(view, opts.active, q, opts.t('notes.switcher.create')),
    completeHeadings: (note, q) => {
      const path = note.trim() ? resolveLink(view.resolver, note, from) : opts.active
      if (!path) {
        return []
      }
      const list = path === opts.active ? opts.headings() : (view.byPath.get(path)?.headings ?? [])
      const lower = q.toLowerCase()
      return list
        .filter((h) => h.text && h.text.toLowerCase().includes(lower))
        .slice(0, 12)
        .map((h) => ({ label: h.text, detail: '#'.repeat(h.level), insert: h.text }))
    },
    completeTags: (q) => {
      const lower = q.toLowerCase()
      return view.tags
        .filter((x) => x.tag.toLowerCase().startsWith(lower) && x.tag.toLowerCase() !== lower)
        .slice(0, 10)
        .map((x) => ({ label: `#${x.tag}`, detail: String(x.count), insert: x.tag }))
    },
    saveAttachment: (name, base64) => vaultSaveAttachment(name, base64),
    t: opts.t,
  }
}
