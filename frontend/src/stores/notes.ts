// notes.ts holds the notes space: the open notes folder and its index, the
// notes open in tabs, the starred and recent ones, and the actions that
// create, rename, move and delete notes. The files on disk are the truth;
// everything here is a view of the index the backend keeps and reloads when
// it reports a change. Other spaces ask for a note through noteRequest, which
// the notes screen takes when it shows.

import { derived, get, writable } from 'svelte/store'
import {
  getSetting,
  listOccurrences,
  setSetting,
  SettingKeys,
  vaultCreate,
  vaultCreateFolder,
  vaultDelete,
  vaultMove,
  vaultOpen,
  vaultRead,
  vaultRescan,
  vaultSnapshot,
  vaultUndo,
  vaultUniquePath,
} from '../lib/api'
import { onVaultChanged } from '../lib/events'
import { createResolver, type Resolver } from '../lib/noteresolve'
import { joinPath, safeNoteName } from '../lib/notemd'
import {
  builtinAboutTemplate,
  builtinDailyTemplate,
  builtinMeetingTemplate,
  dailyNotePath,
  expandTemplate,
  type TemplateEvent,
  type TemplateVars,
} from '../lib/notetemplate'
import { peltonMarkdownLink, type PeltonKind } from '../lib/peltonlinks'
import { addDays, parseEventTime, startOfDay } from '../lib/caldate'
import { dueDate } from '../lib/tasklogic'
import { formatTime } from '../lib/calformat'
import { locale, t } from '../lib/i18n'
import type { TimeFormat } from '../lib/format'
import type { VaultInfo, VaultNote, VaultSnapshot } from '../lib/types'
import { prefs } from './prefs'
import { tasks } from './tasks'
import { openSpace } from './space'
import { errorMessage, pushActions, toastError } from './toast'

/** The open notes folder, or null before it was asked for. */
export const vaultInfo = writable<VaultInfo | null>(null)

/** The index of the open folder, or null before it loaded. */
export const vaultIndex = writable<VaultSnapshot | null>(null)

/** Every note of the open folder. */
export const vaultNotes = derived(vaultIndex, ($i) => $i?.notes ?? [])

/** The notes by path. */
export const notesByPath = derived(vaultNotes, ($n) => new Map($n.map((n) => [n.path, n])))

/** Resolves links against every file of the open folder. */
export const noteResolver = derived(vaultIndex, ($i): Resolver => createResolver([...($i?.notes.map((n) => n.path) ?? []), ...($i?.files ?? [])]))

/** Every tag with the number of notes carrying it, by name. */
export const noteTags = derived(vaultNotes, ($n) => tagCounts($n))

/** Counts the notes per tag; a nested tag also counts towards its parents. */
export function tagCounts(notes: readonly VaultNote[]): { tag: string; count: number }[] {
  const counts = new Map<string, { tag: string; count: number }>()
  for (const n of notes) {
    const mine = new Set<string>()
    for (const tag of n.tags) {
      const parts = tag.split('/')
      for (let i = 1; i <= parts.length; i++) {
        mine.add(parts.slice(0, i).join('/'))
      }
    }
    for (const tag of mine) {
      const key = tag.toLowerCase()
      const c = counts.get(key)
      if (c) {
        c.count++
      } else {
        counts.set(key, { tag, count: 1 })
      }
    }
  }
  return [...counts.values()].sort((a, b) => a.tag.localeCompare(b.tag))
}

/** The notes open in tabs, in order. */
export const openNotes = writable<string[]>([])

/** The note showing in the editor, or null. */
export const activeNote = writable<string | null>(null)

/** A line to scroll the active note to once it shows, or null. */
export const noteLine = writable<{ path: string; line: number } | null>(null)

/** The starred notes of the open folder, in the order they were starred. */
export const starredNotes = writable<string[]>([])

/** The notes opened last, newest first. */
export const recentNotes = writable<string[]>([])

const recentLimit = 12

/** Something another space asks the notes screen to do once it shows. */
export type NoteRequest =
  | { kind: 'new' }
  | { kind: 'today' }
  | { kind: 'switcher' }
  | { kind: 'template' }
  | { kind: 'graph' }
  | { kind: 'toggle-mode' }

/** Text waiting to be inserted at the cursor of the open note, such as links to dropped mail. */
export const noteInsert = writable<string | null>(null)

/** The request waiting for the notes screen, or null. */
export const noteRequest = writable<NoteRequest | null>(null)

/** Shows the notes space and asks it to do something there. */
export function requestNotes(request: NoteRequest): void {
  noteRequest.set(request)
  openSpace('notes')
}

let reloadTimer: ReturnType<typeof setTimeout> | null = null
let pendingReload: Promise<void> | null = null

/** Reloads the index of the open folder. */
export async function reloadVault(): Promise<void> {
  if (pendingReload) {
    return pendingReload
  }
  pendingReload = (async () => {
    try {
      vaultIndex.set(await vaultSnapshot())
    } catch (err) {
      vaultIndex.set(null)
      const msg = errorMessage(err)
      vaultInfo.update((i) => (i ? { ...i, error: i.error || msg } : i))
    } finally {
      pendingReload = null
    }
  })()
  return pendingReload
}

/** Opens the notes folder from the settings, creating the default one on first use, and loads it. */
export async function ensureVault(): Promise<VaultInfo> {
  const info = await vaultOpen()
  vaultInfo.set(info)
  if (!info.error && info.folder) {
    await reloadVault()
    await loadNoteLists(info.folder)
  }
  return info
}

/** Takes the result of choosing another folder: shows it and forgets the old folder's tabs. */
export async function vaultSwitched(info: VaultInfo): Promise<void> {
  if (!info.folder && !info.error) {
    return
  }
  openNotes.set([])
  activeNote.set(null)
  vaultInfo.set(info)
  vaultIndex.set(null)
  if (!info.error) {
    await reloadVault()
    await loadNoteLists(info.folder)
  }
}

let listsFolder = ''

async function loadList(key: string, folder: string): Promise<string[]> {
  try {
    const { value, found } = await getSetting(key)
    if (!found || !value) {
      return []
    }
    const all = JSON.parse(value) as Record<string, string[]>
    return Array.isArray(all[folder]) ? all[folder] : []
  } catch {
    return []
  }
}

async function saveList(key: string, list: string[]): Promise<void> {
  if (!listsFolder) {
    return
  }
  try {
    const { value, found } = await getSetting(key)
    const all = found && value ? (JSON.parse(value) as Record<string, string[]>) : {}
    all[listsFolder] = list
    await setSetting(key, JSON.stringify(all))
  } catch {
    // a lost starred list is not worth an error over.
  }
}

async function loadNoteLists(folder: string): Promise<void> {
  listsFolder = folder
  starredNotes.set(await loadList(SettingKeys.notesStarred, folder))
  recentNotes.set(await loadList(SettingKeys.notesRecent, folder))
}

/** Starts listening for changes to the notes folder; returns the function that stops. */
export function startNotes(): () => void {
  const stop = onVaultChanged((e) => {
    if (reloadTimer) {
      clearTimeout(reloadTimer)
    }
    reloadTimer = setTimeout(() => {
      reloadTimer = null
      void reloadVault().then(() => {
        if (e.reset) {
          vaultInfo.update((i) => (i ? { ...i, ready: true } : i))
        }
      })
    }, 120)
    for (const p of e.removed ?? []) {
      forget(p)
    }
  })
  const onFocus = (): void => {
    if (get(vaultInfo)?.folder) {
      void vaultRescan()
    }
  }
  window.addEventListener('focus', onFocus)
  return () => {
    stop()
    window.removeEventListener('focus', onFocus)
    if (reloadTimer) {
      clearTimeout(reloadTimer)
      reloadTimer = null
    }
  }
}

// forget drops a note that is gone from the starred and recent lists. Its tab
// stays, so the editor can say the note was deleted.
function forget(path: string): void {
  if (get(starredNotes).includes(path)) {
    starredNotes.update((l) => l.filter((p) => p !== path))
    void saveList(SettingKeys.notesStarred, get(starredNotes))
  }
  if (get(recentNotes).includes(path)) {
    recentNotes.update((l) => l.filter((p) => p !== path))
    void saveList(SettingKeys.notesRecent, get(recentNotes))
  }
}

/** Shows a note in the editor: in its own tab when newTab is set, else in place of the active one. */
export function openNote(path: string, options: { newTab?: boolean; line?: number } = {}): void {
  const tabs = get(openNotes)
  const active = get(activeNote)
  if (!tabs.includes(path)) {
    if (options.newTab || !active || !tabs.includes(active)) {
      openNotes.set([...tabs, path])
    } else {
      openNotes.set(tabs.map((p) => (p === active ? path : p)))
    }
  }
  activeNote.set(path)
  if (options.line !== undefined) {
    noteLine.set({ path, line: options.line })
  }
  recentNotes.update((l) => [path, ...l.filter((p) => p !== path)].slice(0, recentLimit))
  void saveList(SettingKeys.notesRecent, get(recentNotes))
}

/** Closes a note's tab; the tab beside it becomes active. */
export function closeNote(path: string): void {
  const tabs = get(openNotes)
  const i = tabs.indexOf(path)
  if (i < 0) {
    return
  }
  const next = tabs.filter((p) => p !== path)
  openNotes.set(next)
  if (get(activeNote) === path) {
    activeNote.set(next[Math.min(i, next.length - 1)] ?? null)
  }
}

/** Stars a note, or unstars it. */
export function toggleStar(path: string): void {
  starredNotes.update((l) => (l.includes(path) ? l.filter((p) => p !== path) : [...l, path]))
  void saveList(SettingKeys.notesStarred, get(starredNotes))
}

// applyMoves follows renamed paths into the tabs and the starred and recent lists.
function applyMoves(moves: Record<string, string>): void {
  const map = (p: string): string => moves[p] ?? p
  openNotes.update((l) => l.map(map))
  activeNote.update((p) => (p ? map(p) : p))
  starredNotes.update((l) => l.map(map))
  recentNotes.update((l) => l.map(map))
  void saveList(SettingKeys.notesStarred, get(starredNotes))
  void saveList(SettingKeys.notesRecent, get(recentNotes))
}

function invert(moves: Record<string, string>): Record<string, string> {
  return Object.fromEntries(Object.entries(moves).map(([a, b]) => [b, a]))
}

/** Creates a note with content in folder under a free name made from title, and opens it. */
export async function createNote(folder: string, title: string, content = '', open = true): Promise<string | null> {
  try {
    const free = await vaultUniquePath(joinPath(folder, `${safeNoteName(title)}.md`))
    const path = await vaultCreate(free, content)
    await reloadVault()
    if (open) {
      openNote(path, { newTab: true })
    }
    return path
  } catch (err) {
    toastError(errorMessage(err))
    return null
  }
}

/** Creates a folder under a free name and returns its path. */
export async function createFolder(parent: string, name: string): Promise<string | null> {
  try {
    const free = await vaultUniquePath(joinPath(parent, safeNoteName(name)))
    const path = await vaultCreateFolder(free)
    await reloadVault()
    return path
  } catch (err) {
    toastError(errorMessage(err))
    return null
  }
}

/**
 * Renames or moves a note or folder. Links to it in other notes follow; the
 * toast says how many notes changed and offers to undo all of it.
 */
export async function moveNote(from: string, to: string): Promise<boolean> {
  if (from === to) {
    return true
  }
  const tr = get(t)
  try {
    const op = await vaultMove(from, to)
    applyMoves(op.moves)
    await reloadVault()
    const n = op.rewritten.length
    const message = n === 0 ? tr('notes.toast.moved') : n === 1 ? tr('notes.toast.movedLinksOne') : tr('notes.toast.movedLinks').replace('{n}', String(n))
    pushActions('success', message, [{ label: tr('notes.undo'), run: () => void undo(op.undoId, invert(op.moves)) }], 8000)
    return true
  } catch (err) {
    toastError(errorMessage(err))
    return false
  }
}

/** Moves a note or folder to the trash folder of the vault, with undo. */
export async function deleteNote(path: string): Promise<boolean> {
  const tr = get(t)
  try {
    const op = await vaultDelete(path)
    closeNote(path)
    for (const p of get(openNotes).filter((x) => x.startsWith(`${path}/`))) {
      closeNote(p)
    }
    await reloadVault()
    pushActions('success', tr('notes.toast.deleted'), [{ label: tr('notes.undo'), run: () => void undo(op.undoId, {}) }], 8000)
    return true
  } catch (err) {
    toastError(errorMessage(err))
    return false
  }
}

async function undo(id: string, moves: Record<string, string>): Promise<void> {
  try {
    await vaultUndo(id)
    applyMoves(moves)
    await reloadVault()
  } catch (err) {
    toastError(errorMessage(err))
  }
}

// --- templates and daily notes ------------------------------------------------------

/** The template notes: every note in the templates folder. */
export const templateNotes = derived([vaultNotes, prefs], ([$n, $p]) => {
  const folder = $p.notesTemplatesFolder.trim().replace(/^\/+|\/+$/g, '')
  if (!folder) {
    return []
  }
  return $n.filter((n) => n.path.startsWith(`${folder}/`)).sort((a, b) => a.title.localeCompare(b.title))
})

/** Reads a template note, or returns '' when it cannot be read. */
export async function readTemplate(path: string): Promise<string> {
  try {
    return (await vaultRead(path)).content
  } catch {
    return ''
  }
}

function baseVars(title: string, date = new Date()): TemplateVars {
  return { date, title, locale: get(locale) }
}

/** Creates a note from a template in folder and opens it. */
export async function createFromTemplate(templatePath: string, folder: string, title: string, extra: Partial<TemplateVars> = {}): Promise<string | null> {
  const template = await readTemplate(templatePath)
  return createNote(folder, title, expandTemplate(template, { ...baseVars(title), ...extra }))
}

/** The vault path of the daily note of a day, with the folder and format from the settings. */
export function dailyPath(date: Date): string {
  const p = get(prefs)
  return dailyNotePath(date, p.notesDailyFolder, p.notesDailyFormat, get(locale))
}

/** The day's events and the tasks due that day, as Markdown list lines linking back to them. */
export async function dayAgenda(date: Date): Promise<{ events: string[]; tasks: string[] }> {
  const from = startOfDay(date)
  const to = addDays(from, 1)
  const timeFormat = get(prefs).timeFormat as TimeFormat
  let events: string[] = []
  try {
    const occ = await listOccurrences(from.toISOString(), to.toISOString())
    events = occ
      .filter((o) => !o.task && o.status.toUpperCase() !== 'CANCELLED')
      .sort((a, b) => a.start.localeCompare(b.start))
      .map((o) => {
        const when = o.allDay ? get(t)('notes.daily.allDay') : formatTime(parseEventTime(o.start, false), timeFormat)
        return `- ${when} ${peltonMarkdownLink('event', o.uid, o.summary || get(t)('calendar.untitled'))}`
      })
  } catch {
    events = []
  }
  const due = get(tasks)
    .filter((x) => {
      const d = dueDate(x)
      return !x.done && d !== null && d >= from && d < to
    })
    .map((x) => `- [ ] ${peltonMarkdownLink('task', x.uid, x.title || get(t)('tasks.untitled'))}`)
  return { events, tasks: due }
}

/**
 * Opens the daily note of a day, creating it from the daily template when it
 * does not exist yet. The template's {{events}} and {{tasks}} fill with the
 * day's agenda at that moment.
 */
export async function openDaily(date: Date): Promise<string | null> {
  const path = dailyPath(date)
  if (get(notesByPath).has(path)) {
    openNote(path)
    return path
  }
  const p = get(prefs)
  const template = p.notesDailyTemplate.trim() ? await readTemplate(p.notesDailyTemplate.trim()) : ''
  const agenda = await dayAgenda(date)
  const content = expandTemplate(template || builtinDailyTemplate, { ...baseVars(noteTitleOf(path), date), ...agenda })
  try {
    const created = await vaultCreate(path, content)
    await reloadVault()
    openNote(created)
    return created
  } catch (err) {
    // made meanwhile by another app or a second click: open what is there.
    await reloadVault()
    if (get(notesByPath).has(path)) {
      openNote(path)
      return path
    }
    toastError(errorMessage(err))
    return null
  }
}

function noteTitleOf(path: string): string {
  return path.slice(path.lastIndexOf('/') + 1).replace(/\.md$/i, '')
}

/** The template note named name (case does not matter) in the templates folder, if any. */
function namedTemplate(name: string): VaultNote | undefined {
  return get(templateNotes).find((n) => n.title.toLowerCase() === name.toLowerCase())
}

/** What a "new note about this" is about. */
export interface NoteAbout {
  kind: PeltonKind
  id: string
  title: string
  /** For an event: what a meeting note fills in. */
  event?: Omit<TemplateEvent, 'link'>
}

/**
 * Makes a note about a mail, event, contact or task and opens it in the notes
 * space. An event gets the "Meeting" template, anything else the "About"
 * one, from the templates folder when it has one.
 */
export async function newNoteAbout(about: NoteAbout): Promise<string | null> {
  const info = get(vaultInfo)?.folder ? get(vaultInfo)! : await ensureVault()
  if (info.error || !info.folder) {
    toastError(info.error || get(t)('notes.noVault'))
    return null
  }
  if (!get(vaultIndex)) {
    await reloadVault()
  }
  const link = peltonMarkdownLink(about.kind, about.id, about.title)
  const meeting = about.kind === 'event'
  const named = namedTemplate(meeting ? 'Meeting' : 'About')
  const template = named ? await readTemplate(named.path) : meeting ? builtinMeetingTemplate : builtinAboutTemplate
  const vars: TemplateVars = { ...baseVars(about.title), link }
  if (about.event) {
    vars.event = { ...about.event, link }
  }
  const content = expandTemplate(template, vars)
  const path = await createNote('', about.title || get(t)('notes.untitled'), content)
  if (path) {
    openSpace('notes')
  }
  return path
}
