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
  onScriptCatchUp,
  onScriptConfirm,
  onScriptRun,
  onScriptsChanged,
  onScriptToast,
  type Unsubscribe,
} from '../lib/events'
import { t } from '../lib/i18n'
import {
  serializeTree,
  type BlockTree,
  type CompileResult,
  type Script,
  type ScriptCatalog,
  type ScriptCatchUp,
  type ScriptConfirmRequest,
  type ScriptFile,
  type ScriptInput,
  type ScriptRun,
} from '../lib/scripting'
import { messageKeys, offerUndo, newestHistoryId, recordHistory } from './undohistory'
import { errorMessage, push, toastError, toastSuccess } from './toast'

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

/** Loads the scripts. */
export async function loadScripts(): Promise<void> {
  scripts.set(await api.listScripts())
  scriptsLoaded.set(true)
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
  if (enabled && s.permissions.some((p) => !s.approved.includes(p))) {
    approvalFor.set(s)
    return
  }
  replace(await api.setScriptEnabled(s.id, enabled, []))
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

/** Dry runs a script as written; the result shows in the editor. */
export async function runTest(input: ScriptInput, messageId: number): Promise<ScriptRun> {
  const run = await api.testScript(input, messageId)
  testRun.set(run)
  return run
}

function reportRun(run: ScriptRun): void {
  const say = get(t)
  if (run.status === 'ok' || run.status === 'stopped') {
    toastSuccess(say('scripts.toast.ran').replace('{name}', run.scriptName))
  } else {
    toastError(say('scripts.toast.failed').replace('{name}', run.scriptName).replace('{error}', run.error || say(`scripts.status.${run.status}`)))
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

/** Stores the previewed script, switched off, and opens it. */
export async function confirmImport(): Promise<Script | null> {
  const file = get(importPreview)
  if (!file) {
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
    }),
    onScriptConfirm((req) => pendingConfirms.update((list) => [...list, req])),
    onScriptCatchUp((c) => pendingCatchUps.update((list) => [...list.filter((x) => x.scriptId !== c.scriptId), c])),
    onScriptToast((e) => push(e.level === 'error' ? 'error' : e.level === 'success' ? 'success' : 'info', `${e.script}: ${e.text}`)),
  ]
  void api
    .pendingScriptCatchUps()
    .then((list) => pendingCatchUps.set(list))
    .catch(() => undefined)
  return () => stops.forEach((stop) => stop())
}
