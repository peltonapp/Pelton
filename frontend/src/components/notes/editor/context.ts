// context.ts is what the note editor's extensions need from the notes space:
// where the open note lives, how links resolve, how to read embedded files
// and notes, and what following a link does. The editor component provides
// it through a facet, so the extensions stay free of stores.

import { Facet } from '@codemirror/state'

/** A link the user followed out of a note. */
export interface FollowedLink {
  kind: 'wiki' | 'markdown' | 'url' | 'tag'
  target: string
  subpath: string
}

/** One entry of the editor's completion list. */
export interface CompletionItem {
  label: string
  /** Second line, such as the folder of a note. */
  detail: string
  /** Text put in place of the typed query. */
  insert: string
  /** Offers to create something that does not exist yet. */
  create?: boolean
}

/** The notes space as the editor sees it. */
export interface NoteContext {
  /** The vault path of the note being edited. */
  path: string
  /** The vault path a link target points at, or ''. markdown links resolve relative to the note. */
  resolve: (target: string, markdown: boolean) => string
  /** A file of the vault as a data: URL. */
  fileData: (path: string) => Promise<string>
  /** The text of another note, for an embed. */
  readNote: (path: string) => Promise<string>
  /** Follows a link; newTab when a modifier was held. */
  follow: (link: FollowedLink, newTab: boolean) => void
  /** Completions for [[, [[Note#, and #tag. */
  completeNotes: (query: string) => CompletionItem[]
  completeHeadings: (note: string, query: string) => CompletionItem[]
  completeTags: (query: string) => CompletionItem[]
  /** Stores a pasted image and returns its vault path. */
  saveAttachment: (name: string, base64: string) => Promise<string>
  /** Translates a key. */
  t: (key: string) => string
}

const empty: NoteContext = {
  path: '',
  resolve: () => '',
  fileData: () => Promise.reject(new Error('no vault')),
  readNote: () => Promise.reject(new Error('no vault')),
  follow: () => {},
  completeNotes: () => [],
  completeHeadings: () => [],
  completeTags: () => [],
  saveAttachment: () => Promise.reject(new Error('no vault')),
  t: (key) => key,
}

/** The notes space the editor is working in. */
export const noteContext = Facet.define<NoteContext, NoteContext>({
  combine: (values) => values[values.length - 1] ?? empty,
})

/** How the editor shows Markdown: rendered while typing, as source, or read-only. */
export type PreviewMode = 'live' | 'source' | 'reading'

/** The mode the editor is in. */
export const previewMode = Facet.define<PreviewMode, PreviewMode>({
  combine: (values) => values[values.length - 1] ?? 'live',
})
