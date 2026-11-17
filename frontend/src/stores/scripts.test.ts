import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import type { Script, ScriptRun } from '../lib/scripting'

const api = vi.hoisted(() => ({
  listScripts: vi.fn(),
  scriptRegistry: vi.fn(),
  scriptRuns: vi.fn(),
  createScript: vi.fn(),
  updateScript: vi.fn(),
  deleteScript: vi.fn(),
  setScriptEnabled: vi.fn(),
  runScript: vi.fn(),
  testScript: vi.fn(),
  undoScriptRun: vi.fn(),
  readScriptFile: vi.fn(),
  importScript: vi.fn(),
  answerScriptConfirm: vi.fn(),
  resolveScriptCatchUp: vi.fn(),
  pendingScriptCatchUps: vi.fn(),
}))

const handlers = vi.hoisted(() => ({}) as Record<string, (payload?: unknown) => void>)

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  ...api,
}))

vi.mock('../lib/events', async (importOriginal) => {
  const real = await importOriginal<typeof import('../lib/events')>()
  const on = (name: string) => (cb: (payload?: unknown) => void) => {
    handlers[name] = cb
    return () => delete handlers[name]
  }
  return {
    ...real,
    onScriptRun: on('run'),
    onScriptsChanged: on('changed'),
    onScriptConfirm: on('confirm'),
    onScriptCatchUp: on('catchup'),
    onScriptToast: on('toast'),
  }
})

const store = await import('./scripts')
const { history, clearHistory, undoLatest } = await import('./undohistory')
const { toasts } = await import('./toast')

function script(over: Partial<Script> = {}): Script {
  return {
    id: 1,
    name: 'Archive invoices',
    description: '',
    kind: 'lua',
    source: '',
    blocks: '',
    trigger: { type: 'manual', params: {} },
    permissions: ['mail.read', 'mail.modify'],
    approved: [],
    enabled: false,
    origin: 'user',
    settingsSchema: [],
    settings: {},
    catchUp: 'live',
    catchUpCap: 50,
    allowUnasked: [],
    createdAt: '',
    updatedAt: '',
    lastRunAt: '',
    ...over,
  }
}

function run(over: Partial<ScriptRun> = {}): ScriptRun {
  return {
    id: 10,
    scriptId: 1,
    scriptName: 'Archive invoices',
    trigger: 'manual',
    dry: false,
    depth: 0,
    started: '2026-11-17T10:00:00Z',
    durationMs: 3,
    status: 'ok',
    error: '',
    steps: [],
    changes: 2,
    cancelled: false,
    cancelReason: '',
    logTruncated: false,
    undoable: true,
    messages: [5],
    ...over,
  }
}

beforeEach(() => {
  Object.values(api).forEach((f) => f.mockReset())
  api.scriptRuns.mockResolvedValue([])
  api.pendingScriptCatchUps.mockResolvedValue([])
  store.scripts.set([])
  store.selectedScriptId.set(null)
  store.approvalFor.set(null)
  store.pendingConfirms.set([])
  store.pendingCatchUps.set([])
  store.importPreview.set(null)
  store.runLog.set([])
  clearHistory()
  toasts.set([])
})

describe('loading and editing', () => {
  it('loads the scripts and opens a new one', async () => {
    api.listScripts.mockResolvedValue([script({ id: 2, name: 'B' })])
    await store.loadScripts()
    api.createScript.mockResolvedValue(script({ id: 3, name: 'A' }))
    await store.createScript({} as never)
    expect(get(store.scripts).map((s) => s.name)).toEqual(['A', 'B'])
    expect(get(store.selectedScript)?.id).toBe(3)
    expect(api.scriptRuns).toHaveBeenCalledWith(3, store.runLogLimit)
  })

  it('drops a deleted script and closes it', async () => {
    store.scripts.set([script()])
    store.selectedScriptId.set(1)
    api.deleteScript.mockResolvedValue(undefined)
    await store.removeScript(1)
    expect(get(store.scripts)).toEqual([])
    expect(get(store.selectedScriptId)).toBeNull()
  })
})

describe('switching on', () => {
  it('asks for approval while permissions are unapproved', async () => {
    const s = script()
    await store.setEnabled(s, true)
    expect(get(store.approvalFor)).toBe(s)
    expect(api.setScriptEnabled).not.toHaveBeenCalled()
  })

  it('approves every declared permission and switches on', async () => {
    const s = script()
    store.scripts.set([s])
    api.setScriptEnabled.mockResolvedValue(script({ enabled: true, approved: s.permissions }))
    await store.approveAndEnable(s)
    expect(api.setScriptEnabled).toHaveBeenCalledWith(1, true, ['mail.read', 'mail.modify'])
    expect(get(store.scripts)[0].enabled).toBe(true)
    expect(get(store.approvalFor)).toBeNull()
  })

  it('switches off without asking', async () => {
    api.setScriptEnabled.mockResolvedValue(script())
    await store.setEnabled(script({ enabled: true, approved: ['mail.read', 'mail.modify'] }), false)
    expect(api.setScriptEnabled).toHaveBeenCalledWith(1, false, [])
  })
})

describe('runs and undo', () => {
  it('records a run once as one undoable history entry', async () => {
    api.runScript.mockResolvedValue(run())
    api.undoScriptRun.mockResolvedValue(undefined)
    const stop = store.startScriptEvents()
    await store.runNow(script({ enabled: true }), 5)
    handlers.run(run())
    const rows = get(history)
    expect(rows).toHaveLength(1)
    expect(rows[0].kind).toBe('script')
    expect(rows[0].count).toBe(1)
    expect(await undoLatest()).toBe(true)
    expect(api.undoScriptRun).toHaveBeenCalledWith(10)
    stop()
  })

  it('records nothing for a dry run or one without changes', () => {
    store.recordRun(run({ id: 11, dry: true }))
    store.recordRun(run({ id: 12, undoable: false }))
    expect(get(history)).toHaveLength(0)
  })

  it('puts live runs into the log on screen', () => {
    const stop = store.startScriptEvents()
    store.selectedScriptId.set(1)
    handlers.run(run({ id: 20, undoable: false }))
    handlers.run(run({ id: 21, scriptId: 2, undoable: false }))
    expect(get(store.runLog).map((r) => r.id)).toEqual([20])
    stop()
  })

  it('reports a failed manual run', async () => {
    api.runScript.mockResolvedValue(run({ status: 'denied', error: 'needs mail.send', undoable: false }))
    await store.runNow(script({ enabled: true }), 0)
    expect(get(toasts).some((t) => t.kind === 'error' && t.message.includes('needs mail.send'))).toBe(true)
  })
})

describe('questions from running scripts', () => {
  it('queues confirmations and answers them', async () => {
    const stop = store.startScriptEvents()
    handlers.confirm({ id: 'a', scriptId: 1, scriptName: 'S', function: 'mail.send', summary: 'mail.send(...)' })
    expect(get(store.pendingConfirms)).toHaveLength(1)
    api.answerScriptConfirm.mockResolvedValue(undefined)
    await store.answerConfirm('a', true, true)
    expect(api.answerScriptConfirm).toHaveBeenCalledWith('a', true, true)
    expect(get(store.pendingConfirms)).toHaveLength(0)
    stop()
  })

  it('keeps one catch-up question per script', async () => {
    const stop = store.startScriptEvents()
    handlers.catchup({ scriptId: 1, name: 'S', count: 3 })
    handlers.catchup({ scriptId: 1, name: 'S', count: 5 })
    expect(get(store.pendingCatchUps)).toEqual([{ scriptId: 1, name: 'S', count: 5 }])
    api.resolveScriptCatchUp.mockResolvedValue(undefined)
    await store.answerCatchUp(1, false)
    expect(api.resolveScriptCatchUp).toHaveBeenCalledWith(1, false)
    expect(get(store.pendingCatchUps)).toEqual([])
    stop()
  })
})

describe('import', () => {
  it('previews a file and stores it only when confirmed', async () => {
    const file = { script: script({ origin: 'import' }), text: '{"format":1}' }
    api.readScriptFile.mockResolvedValue(file)
    await store.previewImport('/tmp/x.peltonscript')
    expect(get(store.importPreview)).toEqual(file)
    expect(api.importScript).not.toHaveBeenCalled()
    api.importScript.mockResolvedValue(script({ id: 9, origin: 'import' }))
    await store.confirmImport()
    expect(api.importScript).toHaveBeenCalledWith('{"format":1}')
    expect(get(store.importPreview)).toBeNull()
    expect(get(store.selectedScriptId)).toBe(9)
  })
})
