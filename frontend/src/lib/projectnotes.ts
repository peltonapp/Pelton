// projectnotes.ts is where a project's notes come from. With a notes folder
// open they are Markdown files in it, put in the project by their front
// matter, and the Notes editor edits them; without one they are kept with the
// project in Pelton's database, as before, until a folder turns up and the
// backend moves them there. The Notes tab and the cards only talk to the
// ProjectNotesSource below. A note's id is what a card's link to it stores:
// the file's path in the notes folder, or the row id of a database note.

import {
  attachProjectNote,
  attachVaultNote,
  createProjectNote,
  createProjectVaultNote,
  deleteProjectNote,
  listProjectNotes,
  listProjectVaultNotes,
  removeNoteFromProject,
  updateProjectNote,
} from './api'
import type { ProjectNoteRecord, ProjectVaultNote } from './types'

/** A note of a project. */
export interface ProjectNote {
  /** Opaque to callers; what a card's link to the note stores. */
  id: string
  projectId: number
  title: string
  /** The Markdown text of a database note; '' for a file, which the Notes editor reads itself. */
  body: string
  /** The opening words, for a list row. */
  snippet: string
  /** The note's path in the notes folder, '' for a database note. */
  path: string
  /** When it was last saved, rfc3339. */
  updatedAt: string
}

/**
 * A project's notes. files says they are files in the notes folder; linked
 * holds the other notes there that link to the project or to its cards,
 * mail and events.
 */
export interface ProjectNoteList {
  files: boolean
  notes: ProjectNote[]
  linked: ProjectNote[]
}

/** Everything the Projects space does with notes. */
export interface ProjectNotesSource {
  /** A project's notes, the last edited first. */
  list(projectId: number): Promise<ProjectNoteList>
  /** Makes a note; body is its text, such as a filled template. */
  create(projectId: number, title: string, body: string): Promise<ProjectNote>
  /** Saves a database note's title and body. A file is saved by the Notes editor. */
  save(note: ProjectNote): Promise<ProjectNote>
  /** Takes a note out of its project: a database note is deleted, a file stays where it is. */
  remove(note: ProjectNote): Promise<void>
  /** Attaches a note to a card of its project. */
  attach(note: ProjectNote, taskUid: string): Promise<void>
}

function fromRecord(r: ProjectNoteRecord): ProjectNote {
  return { id: String(r.id), projectId: r.projectId, title: r.title, body: r.body, snippet: noteExcerpt(r.body), path: '', updatedAt: r.updatedAt }
}

function fromFile(n: ProjectVaultNote, projectId: number): ProjectNote {
  return { id: n.path, projectId, title: n.title, body: '', snippet: n.snippet, path: n.path, updatedAt: new Date(n.mtime).toISOString() }
}

/** Notes kept with the project in Pelton's database. */
export const localProjectNotes: ProjectNotesSource = {
  async list(projectId) {
    return { files: false, notes: (await listProjectNotes(projectId)).map(fromRecord), linked: [] }
  },
  async create(projectId, title, body) {
    return fromRecord(await createProjectNote(projectId, title, body))
  },
  async save(note) {
    return fromRecord(await updateProjectNote(Number(note.id), note.title, note.body))
  },
  async remove(note) {
    await deleteProjectNote(Number(note.id))
  },
  async attach(note, taskUid) {
    await attachProjectNote(Number(note.id), taskUid)
  },
}

/** Notes as files in the notes folder, falling back to the database while no folder is open. */
export const vaultProjectNotes: ProjectNotesSource = {
  async list(projectId) {
    const r = await listProjectVaultNotes(projectId)
    if (!r.available) {
      return localProjectNotes.list(projectId)
    }
    return { files: true, notes: r.notes.map((n) => fromFile(n, projectId)), linked: r.linked.map((n) => fromFile(n, projectId)) }
  },
  async create(projectId, title, body) {
    try {
      return fromFile(await createProjectVaultNote(projectId, title, body), projectId)
    } catch (err) {
      if ((await listProjectVaultNotes(projectId)).available) {
        throw err
      }
      return localProjectNotes.create(projectId, title, body)
    }
  },
  async save(note) {
    if (note.path) {
      throw new Error('a note file is saved by the Notes editor')
    }
    return localProjectNotes.save(note)
  },
  async remove(note) {
    if (note.path) {
      await removeNoteFromProject(note.path, note.projectId)
    } else {
      await localProjectNotes.remove(note)
    }
  },
  async attach(note, taskUid) {
    if (note.path) {
      await attachVaultNote(note.path, taskUid)
    } else {
      await localProjectNotes.attach(note, taskUid)
    }
  },
}

let source: ProjectNotesSource = vaultProjectNotes

/** The notes source in use. */
export function projectNotes(): ProjectNotesSource {
  return source
}

/** Puts another notes source in place; returns the one it replaced. */
export function useProjectNotes(next: ProjectNotesSource): ProjectNotesSource {
  const before = source
  source = next
  return before
}

/** What a note is called: its title, else its first line of text, else the fallback. */
export function noteTitle(note: Pick<ProjectNote, 'title' | 'body'>, fallback: string): string {
  if (note.title.trim()) {
    return note.title.trim()
  }
  const line = note.body
    .split('\n')
    .map((l) => l.replace(/^#+\s*/, '').trim())
    .find(Boolean)
  return line || fallback
}

/** The first lines of a note's text without Markdown marks, for a list row. */
export function noteExcerpt(body: string, max = 140): string {
  const text = body
    .split('\n')
    .map((l) => l.replace(/^(#+|[-*+]|\d+\.|>)\s*/, '').replace(/[*_`]/g, '').trim())
    .filter(Boolean)
    .join(' ')
  return text.length > max ? `${text.slice(0, max - 1).trimEnd()}…` : text
}
