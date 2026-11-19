// projects.ts holds the projects, which conversations belong to them, the
// suggestions waved away, and the board of the project on screen. the list is
// loaded once the new design starts, because the rail, the mail list chips,
// the reader's suggestions and the composer read it as well as the Projects
// space. a change anywhere fires projects:changed, which reloads what it
// touched; a task edited or synced fires calendar:changed, which reloads the
// board and the counts.

import { derived, get, writable } from 'svelte/store'
import {
  addMailToProject,
  addProjectPerson,
  addTaskToProject,
  createProject,
  createProjectCard,
  deleteProject,
  dismissProjectSuggestion,
  getProjectBoard,
  listProjectDismissals,
  listProjectMailMarks,
  listProjects,
  moveProjectCard,
  removeProjectCard,
  removeProjectPerson,
  reorderProjects,
  setProjectStatus,
  updateProject,
} from '../lib/api'
import { onCalendarChanged, onMailNew, onProjectsChanged, type Unsubscribe } from '../lib/events'
import { cardPlace, moveCard } from '../lib/projectlogic'
import { t } from '../lib/i18n'
import { openSpace } from './space'
import { errorMessage, pushActions, toastError, toastInfo } from './toast'
import type { Project, ProjectBoard, ProjectCard, ProjectCardDraft, ProjectDraft } from '../lib/types'

/** The tabs of an open project. */
export type ProjectTab = 'board' | 'overview' | 'mail' | 'notes' | 'people'

/** The tabs in the order they are shown. */
export const projectTabs: ProjectTab[] = ['board', 'overview', 'mail', 'notes', 'people']

/** Every project, archived ones included, in manual order. */
export const projects = writable<Project[]>([])

/** True once the projects have loaded the first time. */
export const projectsLoaded = writable<boolean>(false)

/** The projects being worked on, in manual order. */
export const activeProjects = derived(projects, ($p) => $p.filter((p) => p.status === 'active'))

/** Conversations that belong to projects: thread id to project ids. */
export const projectMailMarks = writable<Map<number, number[]>>(new Map())

/** Conversations whose "add to project" suggestion was dismissed. */
export const projectDismissals = writable<Set<number>>(new Set())

/** The project on screen in the Projects space, null for the list of projects. */
export const openProjectId = writable<number | null>(null)

/** The tab the open project shows. */
export const projectTab = writable<ProjectTab>('board')

/** The board of the open project, null until it has loaded. */
export const board = writable<ProjectBoard | null>(null)

/** A card to open once its board shows, by task UID. */
export const cardFocus = writable<string | null>(null)

/** Asks the Projects space to open the new-project dialog when it shows. */
export const newProjectRequest = writable<boolean>(false)

/**
 * Asks the app to file the open or selected mail under a project, for callers
 * that do not know which mail that is, such as the command palette.
 */
export const fileMailRequest = writable<boolean>(false)

/** Loads every project. */
export async function loadProjects(): Promise<void> {
  try {
    projects.set(await listProjects())
    projectsLoaded.set(true)
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Loads which conversations belong to which projects. */
export async function loadProjectMarks(): Promise<void> {
  try {
    const marks = new Map<number, number[]>()
    for (const m of await listProjectMailMarks()) {
      marks.set(m.threadId, [...(marks.get(m.threadId) ?? []), m.projectId])
    }
    projectMailMarks.set(marks)
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Loads the dismissed suggestions. */
export async function loadDismissals(): Promise<void> {
  try {
    projectDismissals.set(new Set(await listProjectDismissals()))
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Loads the board of a project; a board for another project is dropped first. */
export async function loadBoard(id: number): Promise<void> {
  if (get(board)?.project.id !== id) {
    board.set(null)
  }
  try {
    const next = await getProjectBoard(id)
    if (get(openProjectId) === id) {
      board.set(next)
    }
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** The project with an id, if it is loaded. */
export function projectById(id: number): Project | undefined {
  return get(projects).find((p) => p.id === id)
}

/** Shows a project in the Projects space, on a tab. */
export function openProject(id: number, tab: ProjectTab = 'board'): void {
  if (get(openProjectId) !== id) {
    board.set(null)
  }
  openProjectId.set(id)
  projectTab.set(tab)
  openSpace('projects')
  void loadBoard(id)
}

/** Goes back to the list of projects. */
export function closeProject(): void {
  openProjectId.set(null)
  board.set(null)
}

/** Shows a card on its board, opening its project. */
export function openCard(projectId: number, taskUid: string): void {
  cardFocus.set(taskUid)
  openProject(projectId, 'board')
}

function replaceProject(p: Project): void {
  projects.update((list) => (list.some((x) => x.id === p.id) ? list.map((x) => (x.id === p.id ? p : x)) : [...list, p]))
  board.update((b) => (b && b.project.id === p.id ? { ...b, project: p } : b))
}

/** Saves a project: creates it for id 0, else updates it. Returns it, or null when that failed. */
export async function saveProject(draft: ProjectDraft): Promise<Project | null> {
  try {
    const saved = draft.id === 0 ? await createProject(draft) : await updateProject(draft)
    replaceProject(saved)
    return saved
  } catch (err) {
    toastError(errorMessage(err))
    return null
  }
}

/** Archives a project or makes it active again. */
export async function setArchived(p: Project, archived: boolean): Promise<void> {
  const status = archived ? 'archived' : 'active'
  replaceProject({ ...p, status })
  try {
    await setProjectStatus(p.id, status)
    const tr = get(t)
    if (archived) {
      pushActions('success', tr('projects.archived').replace('{name}', p.name), [{ label: tr('tasks.undo'), run: () => void setArchived(p, false) }], 6000)
    }
  } catch (err) {
    replaceProject(p)
    toastError(errorMessage(err))
  }
}

/** Deletes a project. The caller asks first; tasks, mail and events stay. */
export async function removeProject(p: Project): Promise<boolean> {
  const before = get(projects)
  projects.update((list) => list.filter((x) => x.id !== p.id))
  try {
    await deleteProject(p.id)
    if (get(openProjectId) === p.id) {
      closeProject()
    }
    void loadProjectMarks()
    return true
  } catch (err) {
    projects.set(before)
    toastError(errorMessage(err))
    return false
  }
}

/** Gives the projects a new manual order. */
export async function reorder(ids: number[]): Promise<void> {
  const byId = new Map(get(projects).map((p) => [p.id, p]))
  const ordered = ids.map((id) => byId.get(id)).filter((p): p is Project => !!p)
  const rest = get(projects).filter((p) => !ids.includes(p.id))
  projects.set([...ordered, ...rest].map((p, i) => ({ ...p, position: i })))
  try {
    await reorderProjects(ids)
  } catch (err) {
    toastError(errorMessage(err))
    await loadProjects()
  }
}

function setCards(update: (cards: ProjectCard[]) => ProjectCard[]): void {
  board.update((b) => (b ? { ...b, cards: update(b.cards) } : b))
}

function replaceCard(card: ProjectCard): void {
  setCards((cards) => (cards.some((c) => c.task.uid === card.task.uid) ? cards.map((c) => (c.task.uid === card.task.uid ? card : c)) : [...cards, card]))
}

/**
 * Moves a card to index in a column. The board changes at once; dropping in
 * the done column completes the task, and a repeating task that moved on to
 * its next date says so and goes back.
 */
export async function moveBoardCard(uid: string, columnId: number, index: number): Promise<void> {
  const current = get(board)
  if (!current) {
    return
  }
  const place = cardPlace(current.cards, uid)
  if (place && place.columnId === columnId && place.index === index) {
    return
  }
  setCards((cards) => moveCard(cards, uid, columnId, index))
  try {
    const result = await moveProjectCard(uid, columnId, index)
    if (result.advanced) {
      const tr = get(t)
      toastInfo(tr('tasks.advanced').replace('{title}', result.card.task.title || tr('tasks.untitled')))
      if (place) {
        setCards((cards) => moveCard(cards, uid, place.columnId, place.index))
      }
    }
    replaceCard(result.card)
  } catch (err) {
    if (get(board)?.project.id === current.project.id) {
      board.set(current)
    }
    toastError(errorMessage(err))
  }
}

/** Makes a task as a card at the end of a column, or at draft.index. */
export async function addCard(draft: ProjectCardDraft): Promise<ProjectCard | null> {
  try {
    const card = await createProjectCard(draft)
    if (get(board)?.project.id === draft.projectId) {
      replaceCard(card)
      setCards((cards) => moveCard(cards, card.task.uid, card.columnId, card.position))
    }
    return card
  } catch (err) {
    toastError(errorMessage(err))
    return null
  }
}

/** Puts an existing task on a board; columnId 0 lets the board pick. */
export async function addTask(projectId: number, taskUid: string, columnId = 0): Promise<ProjectCard | null> {
  try {
    const card = await addTaskToProject(projectId, taskUid, columnId)
    if (get(board)?.project.id === projectId) {
      replaceCard(card)
    }
    return card
  } catch (err) {
    toastError(errorMessage(err))
    return null
  }
}

/** Takes a card off its board, keeping the task, and offers to put it back. */
export async function removeCard(card: ProjectCard): Promise<void> {
  const place = get(board) ? cardPlace(get(board)!.cards, card.task.uid) : null
  setCards((cards) => cards.filter((c) => c.task.uid !== card.task.uid))
  try {
    await removeProjectCard(card.task.uid)
    const tr = get(t)
    pushActions('success', tr('projects.card.removed'), [
      {
        label: tr('tasks.undo'),
        run: () =>
          void (async () => {
            const back = await addTask(card.projectId, card.task.uid, card.columnId)
            if (back && place) {
              await moveBoardCard(card.task.uid, place.columnId, place.index)
            }
          })(),
      },
    ], 8000)
  } catch (err) {
    replaceCard(card)
    toastError(errorMessage(err))
  }
}

/**
 * Files mail under a project, or one of its cards, and says so. The whole
 * conversation of each message follows. Returns how many conversations went in.
 */
export async function fileMail(projectId: number, messageIds: number[], taskUid = ''): Promise<number> {
  if (messageIds.length === 0) {
    return 0
  }
  try {
    const added = await addMailToProject(projectId, messageIds, taskUid)
    const tr = get(t)
    const name = projectById(projectId)?.name ?? ''
    pushActions(
      'success',
      (added === 1 ? tr('projects.mail.addedOne') : tr('projects.mail.addedMany').replace('{n}', String(added))).replace('{name}', name),
      [{ label: tr('projects.open'), run: () => openProject(projectId, taskUid ? 'board' : 'mail') }],
      6000,
    )
    void loadProjectMarks()
    return added
  } catch (err) {
    toastError(errorMessage(err))
    return 0
  }
}

/** Stops a conversation being suggested for a project again. */
export async function dismissSuggestion(threadId: number): Promise<void> {
  projectDismissals.update((s) => new Set([...s, threadId]))
  try {
    await dismissProjectSuggestion(threadId)
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Adds someone to a project. */
export async function addPerson(projectId: number, email: string, name: string): Promise<boolean> {
  try {
    await addProjectPerson(projectId, email, name)
    await loadProjects()
    return true
  } catch (err) {
    toastError(errorMessage(err))
    return false
  }
}

/** Takes someone off a project. */
export async function removePerson(projectId: number, email: string): Promise<void> {
  projects.update((list) => list.map((p) => (p.id === projectId ? { ...p, people: p.people.filter((x) => x.email !== email) } : p)))
  try {
    await removeProjectPerson(projectId, email)
  } catch (err) {
    toastError(errorMessage(err))
    await loadProjects()
  }
}

let started = false
let reloadTimer: ReturnType<typeof setTimeout> | null = null
let boardDirty = false

// many changes come in bursts (a sync, a drag that completes a task), so the
// reloads they ask for are gathered into one.
function scheduleReload(withBoard: boolean): void {
  boardDirty = boardDirty || withBoard
  if (reloadTimer) {
    return
  }
  reloadTimer = setTimeout(() => {
    reloadTimer = null
    const open = get(openProjectId)
    void loadProjects()
    void loadProjectMarks()
    if (boardDirty && open !== null) {
      void loadBoard(open)
    }
    boardDirty = false
  }, 120)
}

/**
 * Loads the projects and keeps them current. Returns the function that stops
 * it.
 */
export function startProjects(): Unsubscribe {
  if (started) {
    return () => undefined
  }
  started = true
  void loadProjects()
  void loadProjectMarks()
  void loadDismissals()
  const offs = [
    onProjectsChanged((e) => {
      const open = get(openProjectId)
      scheduleReload(open !== null && (e.projectIds.length === 0 || e.projectIds.includes(open)))
    }),
    onCalendarChanged(() => scheduleReload(true)),
    onMailNew(() => void loadProjectMarks()),
  ]
  return () => {
    started = false
    offs.forEach((off) => off())
    if (reloadTimer) {
      clearTimeout(reloadTimer)
      reloadTimer = null
    }
  }
}
