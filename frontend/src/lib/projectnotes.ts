// projectnotes.ts is where a project's notes come from. today they are kept
// with the project in Pelton's own database; the Notes tab and the cards only
// talk to the ProjectNotesSource below, so a source that reads Markdown files
// from the Notes space can take its place without touching them. a note's id
// is a string for that reason: a row id now, a file path then.

import {
  attachProjectNote,
  createProjectNote,
  deleteProjectNote,
  listProjectNotes,
  updateProjectNote,
} from './api'
import type { ProjectNoteRecord } from './types'

/** A note of a project: a title and a Markdown body. */
export interface ProjectNote {
  /** Opaque to callers; what a card's link to the note stores. */
  id: string
  projectId: number
  title: string
  body: string
  /** When it was last saved, rfc3339. */
  updatedAt: string
}

/** Everything the Projects space does with notes. */
export interface ProjectNotesSource {
  /** A project's notes, the last edited first. */
  list(projectId: number): Promise<ProjectNote[]>
  /** Makes a note. */
  create(projectId: number, title: string, body: string): Promise<ProjectNote>
  /** Saves a note's title and body. */
  save(note: ProjectNote): Promise<ProjectNote>
  /** Deletes a note, and the cards' links to it. */
  remove(note: ProjectNote): Promise<void>
  /** Attaches a note to a card of its project. */
  attach(note: ProjectNote, taskUid: string): Promise<void>
}

function fromRecord(r: ProjectNoteRecord): ProjectNote {
  return { id: String(r.id), projectId: r.projectId, title: r.title, body: r.body, updatedAt: r.updatedAt }
}

/** Notes kept with the project in Pelton's database. */
export const localProjectNotes: ProjectNotesSource = {
  async list(projectId) {
    return (await listProjectNotes(projectId)).map(fromRecord)
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

let source: ProjectNotesSource = localProjectNotes

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
