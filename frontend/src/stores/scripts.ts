// scripts.ts is the state of the Automations space: the scripts, the registry
// they are built from, the run log, the test run on screen, and the questions
// scripts ask while they run (a risky call, a catch-up). It also turns a run's
// mail changes into one undo history entry.
//
// The block editor builds on this store: it reads `catalog` for its palette,
// keeps the tree in a script input's `blocks`, and calls `compileBlocks` to
// show the generated Lua.

import type { ComponentType } from 'svelte'
import { derived, get, writable, type Readable } from 'svelte/store'
import * as api from '../lib/api'
import {
  onScriptAsk,
  onScriptAskDone,
  onScriptCatchUp,
  onScriptConfirm,
  onScriptProgress,
  onScriptResult,
  onScriptRun,
  onScriptsChanged,
  onScriptToast,
  type Unsubscribe,
} from '../lib/events'
import { t } from '../lib/i18n'
import { validate } from '../components/automations/blocks/validate'
import {
  parseTree,
  serializeTree,
  type BlockTree,
  type CompileResult,
  type LiveRun,
  type PlacedItem,
  type PlacedScript,
  type PlacementConfig,
  type Script,
  type ScriptAsk,
  type ScriptCatalog,
  type ScriptCatchUp,
  type ScriptConfirmRequest,
  type ScriptFile,
  type ScriptInput,
  type ScriptLibrary,
  type ScriptLibraryInput,
  type ScriptResult,
  type ScriptRun,
} from '../lib/scripting'
import { messageKeys, offerUndo, newestHistoryId, recordHistory } from './undohistory'
import { errorMessage, push, toastError, toastInfo, toastSuccess } from './toast'

/** How many runs the log pane loads. */
export const runLogLimit = 200

/** Every script, by name. */
export const scripts = writable<Script[]>([])

/** The registry, once loaded. */
export const catalog = writable<ScriptCatalog | null>(null)

/** The script open in the editor, or null. */
export const selectedScriptId = writable<number | null>(null)

/** The script open in the editor. */
export const selectedScript: Readable<Script | null> = derived(
  [scripts, selectedScriptId],
  ([$scripts, $id]) => $scripts.find((s) => s.id === $id) ?? null,
)

/** The run log on screen: the selected script's runs, or every script's. */
export const runLog = writable<ScriptRun[]>([])

/** The last test run, shown in the editor until the next one. */
export const testRun = writable<ScriptRun | null>(null)

/** Risky calls waiting for the user, oldest first. */
export const pendingConfirms = writable<ScriptConfirmRequest[]>([])

/** Scripts asking whether to run over mail that arrived while Pelton was closed. */
export const pendingCatchUps = writable<ScriptCatchUp[]>([])

/** Questions running scripts ask, oldest first; one dialog shows at a time. */
export const pendingAsks = writable<ScriptAsk[]>([])

/** Results scripts showed, oldest first; the result panel shows the first. */
export const scriptResults = writable<ScriptResult[]>([])

/** Runs in progress that take a moment, for the progress indicator. */
export const liveRuns = writable<LiveRun[]>([])

/** The scripts offered at each placement, by placement id. */
export const placedScripts = writable<Record<string, PlacedScript[]>>({})

/** A script file read for import, waiting for the user to look at it. */
export const importPreview = writable<ScriptFile | null>(null)

/** The script whose permissions the user is asked to approve before it is switched on. */
export const approvalFor = writable<Script | null>(null)

/** Whether the scripts have been loaded once. */
export const scriptsLoaded = writable(false)

/**
 * The block editor's mount point. The editor pane of a block script renders
 * this component when one is set, with the props `tree` (BlockTree),
 * `catalog` (ScriptCatalog) and `trigger` (the trigger id), and listens for
 * its `change` event carrying the new BlockTree. Until a block editor sets
 * it, the pane shows the compiled Lua only.
 */
export const blockEditor = writable<ComponentType | null>(null)

function replace(s: Script): Script {
  scripts.update((list) => {
    const i = list.findIndex((x) => x.id === s.id)
    if (i < 0) {
      return [...list, s].sort((a, b) => a.name.localeCompare(b.name))
    }
    const next = [...list]
    next[i] = s
    return next
  })
  return s
}

/** Loads the scripts, and where each is offered. */
export async function loadScripts(): Promise<void> {
  scripts.set(await api.listScripts())
  scriptsLoaded.set(true)
  await loadPlaced().catch(() => undefined)
}

/** Loads which scripts are offered at each placement. */
export async function loadPlaced(): Promise<void> {
  placedScripts.set(await api.scriptPlacements())
}

/** Sets where in the app a script is offered. */
export async function setPlacements(id: number, list: PlacementConfig[]): Promise<void> {
  replace(await api.setScriptPlacements(id, list))
  await loadPlaced()
}

/** Runs a script from a placement on the items it was used on, and reports how it went. */
export async function runPlaced(scriptId: number, placement: string, items: PlacedItem[]): Promise<ScriptRun | null> {
  const since = newestHistoryId()
  try {
    const run = await api.runPlacedScript(scriptId, placement, items)
    reportRun(run)
    recordRun(run)
    offerUndo(since)
    return run
  } catch (err) {
    toastError(errorMessage(err))
    return null
  }
}

/** Answers a question; cancelled ends the run that asked. */
export async function answerAsk(id: string, value: unknown, cancelled: boolean): Promise<void> {
  pendingAsks.update((list) => list.filter((a) => a.id !== id))
  await api.answerScriptAsk(id, value, cancelled)
}

/** Cancels a run in progress. */
export async function cancelLive(id: number): Promise<void> {
  try {
    await api.cancelScriptRun(id)
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Closes the result on screen. */
export function dismissResult(id: string): void {
  scriptResults.update((list) => list.filter((r) => r.id !== id))
}

/** Loads the registry once. */
export async function loadCatalog(): Promise<ScriptCatalog> {
  const have = get(catalog)
  if (have) {
    return have
  }
  const c = await api.scriptRegistry()
  catalog.set(c)
  return c
}

/** Loads the run log of the selected script, or of all of them with none selected. */
export async function loadRuns(): Promise<void> {
  runLog.set(await api.scriptRuns(get(selectedScriptId) ?? 0, runLogLimit))
}

/** Opens a script in the editor and its runs in the log. */
export function selectScript(id: number | null): void {
  selectedScriptId.set(id)
  if (id !== null) {
    selectedLibraryId.set(null)
    galleryOpen.set(false)
  }
  testRun.set(null)
  void loadRuns().catch((err) => toastError(errorMessage(err)))
}

/** Stores a new script and opens it. */
export async function createScript(input: ScriptInput): Promise<Script> {
  const s = replace(await api.createScript(input))
  selectScript(s.id)
  return s
}

/** Saves the script's content. */
export async function saveScript(id: number, input: ScriptInput): Promise<Script> {
  return replace(await api.updateScript(id, input))
}

/** Deletes a script. */
export async function removeScript(id: number): Promise<void> {
  await api.deleteScript(id)
  scripts.update((list) => list.filter((s) => s.id !== id))
  if (get(selectedScriptId) === id) {
    selectScript(null)
  }
}

/**
 * Switches a script on or off. Switching on with permissions still to approve
 * opens the approval dialog instead; approveAndEnable finishes it.
 */
export async function setEnabled(s: Script, enabled: boolean): Promise<void> {
  const problems = enabled ? blockProblems(s) : 0
  if (problems > 0) {
    throw new Error(get(t)('scripts.blocks.enableBlocked').replace('{n}', String(problems)))
  }
  if (enabled && s.permissions.some((p) => !s.approved.includes(p))) {
    approvalFor.set(s)
    return
  }
  replace(await api.setScriptEnabled(s.id, enabled, []))
}

/** How many problems the block editor finds in a block script, 0 for a Lua script or before the registry loaded. */
export function blockProblems(s: Pick<Script, 'kind' | 'blocks' | 'trigger' | 'settingsSchema'>): number {
  const c = get(catalog)
  if (s.kind !== 'blocks' || !c) {
    return 0
  }
  return validate(parseTree(s.blocks), { catalog: c, trigger: s.trigger.type, settings: s.settingsSchema }).length
}

/** Approves a script's permissions and switches it on. */
export async function approveAndEnable(s: Script): Promise<Script> {
  approvalFor.set(null)
  return replace(await api.setScriptEnabled(s.id, true, s.permissions))
}

/** Lets a script make a risky call without asking, or takes that back. */
export async function setAllowUnasked(id: number, fn: string, allow: boolean): Promise<void> {
  replace(await api.setScriptAllowUnasked(id, fn, allow))
}

/** Sets what a mail script does with mail that arrived while Pelton was closed. */
export async function setCatchUp(id: number, mode: string, cap: number): Promise<void> {
  replace(await api.setScriptCatchUp(id, mode, cap))
}

/** Stores the values of a script's settings. */
export async function setSettings(id: number, values: Record<string, unknown>): Promise<void> {
  replace(await api.setScriptSettings(id, values))
}

/** Turns a block script into a Lua script for good. */
export async function convertToLua(id: number): Promise<Script> {
  return replace(await api.convertScriptToLua(id))
}

/** Compiles a block tree to Lua for the read-only view, with the problems found. */
export function compileBlocks(tree: BlockTree, trigger: string): Promise<CompileResult> {
  return api.compileScriptBlocks(serializeTree(tree), trigger)
}

/** Runs a script now and reports how it went. */
export async function runNow(s: Script, messageId: number): Promise<ScriptRun | null> {
  const since = newestHistoryId()
  try {
    const run = await api.runScript(s.id, messageId)
    reportRun(run)
    recordRun(run)
    offerUndo(since)
    return run
  } catch (err) {
    toastError(errorMessage(err))
    return null
  }
}

/**
 * Dry runs a script as written; the result shows in the editor. With askModel its AI steps really ask a
 * model the user approved for the saved script scriptId.
 */
export async function runTest(input: ScriptInput, messageId: number, askModel = false, scriptId = 0): Promise<ScriptRun> {
  const run = askModel ? await api.testScriptAskingAI(scriptId, input, messageId) : await api.testScript(input, messageId)
  testRun.set(run)
  return run
}

/** Every shared library, by name. */
export const libraries = writable<ScriptLibrary[]>([])

/** The library open in the editor, or null. */
export const selectedLibraryId = writable<number | null>(null)

/** The library open in the editor. */
export const selectedLibrary: Readable<ScriptLibrary | null> = derived(
  [libraries, selectedLibraryId],
  ([$libraries, $id]) => $libraries.find((l) => l.id === $id) ?? null,
)

/** Whether the template gallery is open in the editor pane. */
export const galleryOpen = writable(false)

function replaceLibrary(l: ScriptLibrary): ScriptLibrary {
  libraries.update((list) => [...list.filter((x) => x.id !== l.id), l].sort((a, b) => a.name.localeCompare(b.name)))
  return l
}

/** Loads the shared libraries. */
export async function loadLibraries(): Promise<void> {
  libraries.set(await api.listScriptLibraries())
}

/** Opens a library in the editor, or closes it with null. */
export function selectLibrary(id: number | null): void {
  selectedLibraryId.set(id)
  if (id !== null) {
    selectedScriptId.set(null)
    galleryOpen.set(false)
  }
}

/** Opens the template gallery in the editor pane. */
export function openGallery(): void {
  selectedScriptId.set(null)
  selectedLibraryId.set(null)
  galleryOpen.set(true)
}

/** Stores a new library and opens it. */
export async function createLibrary(input: ScriptLibraryInput): Promise<ScriptLibrary> {
  const l = replaceLibrary(await api.createScriptLibrary(input))
  selectLibrary(l.id)
  return l
}

/** Saves a library. */
export async function saveLibrary(id: number, input: ScriptLibraryInput): Promise<ScriptLibrary> {
  return replaceLibrary(await api.updateScriptLibrary(id, input))
}

/** Deletes a library. */
export async function removeLibrary(id: number): Promise<void> {
  await api.deleteScriptLibrary(id)
  libraries.update((list) => list.filter((l) => l.id !== id))
  if (get(selectedLibraryId) === id) {
    selectedLibraryId.set(null)
  }
}

/** Writes a library to a file the user picks. */
export async function exportLibrary(id: number): Promise<void> {
  if (await api.exportScriptLibrary(id)) {
    toastSuccess(get(t)('scripts.toast.libraryExported'))
  }
}

/** Makes a script from a gallery template, switched off, and opens it. */
export async function useTemplate(id: string): Promise<Script> {
  const s = replace(await api.useScriptTemplate(id))
  galleryOpen.set(false)
  selectScript(s.id)
  toastSuccess(get(t)('scripts.gallery.created').replace('{name}', s.name))
  return s
}

/** Where a failed run stopped, for its error message: the Lua line or the block, or ''. */
export function errorPlace(run: Pick<ScriptRun, 'errorLine' | 'errorBlock' | 'errorLibrary'>, say: (key: string) => string): string {
  if (run.errorLine > 0 && run.errorLibrary) {
    return say('scripts.error.libraryLine').replace('{n}', String(run.errorLine)).replace('{name}', run.errorLibrary)
  }
  if (run.errorLine > 0) {
    return say('scripts.error.line').replace('{n}', String(run.errorLine))
  }
  if (run.errorBlock) {
    return say('scripts.error.block').replace('{id}', run.errorBlock)
  }
  return ''
}

function reportRun(run: ScriptRun): void {
  const say = get(t)
  if (run.status === 'ok' || run.status === 'stopped') {
    toastSuccess(say('scripts.toast.ran').replace('{name}', run.scriptName))
  } else if (run.status === 'cancelled') {
    toastInfo(say('scripts.toast.cancelled').replace('{name}', run.scriptName))
  } else {
    const where = errorPlace(run, say)
    const error = run.error || say(`scripts.status.${run.status}`)
    toastError(say('scripts.toast.failed').replace('{name}', run.scriptName).replace('{error}', where ? `${where}: ${error}` : error))
  }
}

/** Writes a script to a file the user picks. */
export async function exportToFile(id: number): Promise<void> {
  const path = await api.exportScript(id)
  if (path) {
    toastSuccess(get(t)('scripts.toast.exported'))
  }
}

/** Reads a script file and opens the import dialog on it. */
export async function previewImport(path: string): Promise<void> {
  try {
    importPreview.set(await api.readScriptFile(path))
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Asks for a script file and opens the import dialog on it. */
export async function pickImport(): Promise<void> {
  const path = await api.pickScriptFile()
  if (path) {
    await previewImport(path)
  }
}

/** Stores the previewed script, switched off, or the previewed library, and opens it. */
export async function confirmImport(): Promise<Script | null> {
  const file = get(importPreview)
  if (!file) {
    return null
  }
  if (file.library) {
    const l = replaceLibrary(await api.importScriptLibrary(file.text))
    importPreview.set(null)
    selectLibrary(l.id)
    toastSuccess(get(t)('scripts.toast.imported').replace('{name}', l.name))
    return null
  }
  const s = replace(await api.importScript(file.text))
  importPreview.set(null)
  selectScript(s.id)
  toastSuccess(get(t)('scripts.toast.imported').replace('{name}', s.name))
  return s
}

/** Answers the oldest risky call waiting. */
export async function answerConfirm(id: string, allow: boolean, always: boolean): Promise<void> {
  pendingConfirms.update((list) => list.filter((r) => r.id !== id))
  await api.answerScriptConfirm(id, allow, always)
}

/** Answers a catch-up question. */
export async function answerCatchUp(scriptId: number, run: boolean): Promise<void> {
  pendingCatchUps.update((list) => list.filter((c) => c.scriptId !== scriptId))
  await api.resolveScriptCatchUp(scriptId, run)
}

// the runs already in the undo history: a manual run is recorded by its
// caller and again announced by the backend's event.
const recorded = new Set<number>()

/** Records a run's mail changes as one undo history entry, once. */
export function recordRun(run: ScriptRun): void {
  if (!run.undoable || run.dry || recorded.has(run.id)) {
    return
  }
  recorded.add(run.id)
  const say = get(t)
  recordHistory({
    kind: 'script',
    label: say('scripts.history.label').replace('{name}', run.scriptName),
    detail: say('scripts.history.detail').replace('{n}', String(run.changes)),
    count: run.messages.length,
    touches: run.messages.flatMap(messageKeys),
    undo: () => api.undoScriptRun(run.id),
    rekeys: true,
  })
}

/** Whether the run belongs in the log on screen. */
function inView(run: ScriptRun): boolean {
  const id = get(selectedScriptId)
  return !run.dry && (id === null || id === run.scriptId)
}

/**
 * Subscribes to the backend's script events for the life of the app: runs go
 * into the log and the undo history, questions into their queues, toasts onto
 * the screen. Returns the function that unsubscribes.
 */
export function startScriptEvents(): Unsubscribe {
  const stops = [
    onScriptRun((run) => {
      if (run.dry) {
        return
      }
      recordRun(run)
      if (inView(run)) {
        runLog.update((list) => [run, ...list.filter((r) => r.id !== run.id)].slice(0, runLogLimit))
      }
      scripts.update((list) => list.map((s) => (s.id === run.scriptId && run.status !== 'skipped' ? { ...s, lastRunAt: run.started } : s)))
    }),
    onScriptsChanged(() => {
      void loadScripts().catch(() => undefined)
      void loadLibraries().catch(() => undefined)
    }),
    onScriptConfirm((req) => pendingConfirms.update((list) => [...list, req])),
    onScriptCatchUp((c) => pendingCatchUps.update((list) => [...list.filter((x) => x.scriptId !== c.scriptId), c])),
    onScriptToast((e) => push(e.level === 'error' ? 'error' : e.level === 'success' ? 'success' : 'info', `${e.script}: ${e.text}`)),
    onScriptAsk((a) => pendingAsks.update((list) => [...list, a])),
    onScriptAskDone((id) => pendingAsks.update((list) => list.filter((a) => a.id !== id))),
    onScriptResult((r) => scriptResults.update((list) => [...list, r])),
    onScriptProgress((run) => liveRuns.update((list) => (run.done ? list.filter((r) => r.id !== run.id) : [...list.filter((r) => r.id !== run.id), run]))),
  ]
  void api
    .pendingScriptCatchUps()
    .then((list) => pendingCatchUps.set(list))
    .catch(() => undefined)
  void api
    .scriptLiveRuns()
    .then((list) => liveRuns.set(list))
    .catch(() => undefined)
  return () => stops.forEach((stop) => stop())
}
