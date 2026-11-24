import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import { catalogFixture, scriptFixture } from '../../test/scriptfixtures'

const api = vi.hoisted(() => ({
  setScriptEnabled: vi.fn(),
  answerScriptConfirm: vi.fn(),
  resolveScriptCatchUp: vi.fn(),
  pendingScriptCatchUps: vi.fn(),
  listScripts: vi.fn(),
  scriptRuns: vi.fn(),
  checkScriptLua: vi.fn(),
  compileScriptBlocks: vi.fn(),
  updateScript: vi.fn(),
  testScript: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

const { default: ScriptList } = await import('./ScriptList.svelte')
const { default: PermissionList } = await import('./PermissionList.svelte')
const { default: RunSteps } = await import('./RunSteps.svelte')
const { default: AutomationsHost } = await import('./AutomationsHost.svelte')
const { default: ScriptEditor } = await import('./ScriptEditor.svelte')
const store = await import('../../stores/scripts')
const { prefs } = await import('../../stores/prefs')

beforeEach(() => {
  prefs.update((p) => ({ ...p, reduceMotion: true }))
  Object.values(api).forEach((f) => f.mockReset())
  api.pendingScriptCatchUps.mockResolvedValue([])
  api.listScripts.mockResolvedValue([])
  api.scriptRuns.mockResolvedValue([])
  api.checkScriptLua.mockResolvedValue('')
  api.compileScriptBlocks.mockResolvedValue({ lua: 'mail.flag(event.message, true)\n', errors: [], permissions: ['mail.modify'] })
  store.scripts.set([])
  store.catalog.set(catalogFixture)
  store.selectedScriptId.set(null)
  store.approvalFor.set(null)
  store.pendingConfirms.set([])
  store.pendingCatchUps.set([])
  store.importPreview.set(null)
  store.testRun.set(null)
})

describe('ScriptList', () => {
  it('shows the empty state with both kinds of new script', () => {
    render(ScriptList)
    expect(screen.getByText('No scripts yet')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'New block script' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'New Lua script' })).toBeInTheDocument()
  })

  it('lists scripts with their trigger and origin, and opens one', async () => {
    store.scripts.set([scriptFixture({ id: 1, name: 'Rules' }), scriptFixture({ id: 2, name: 'Shared', origin: 'import' })])
    render(ScriptList)
    const row = screen.getByRole('button', { name: /Shared/ })
    expect(within(row).getByText('Imported')).toBeInTheDocument()
    expect(within(row).getByText(/Mail arrives/)).toBeInTheDocument()
    await userEvent.click(row)
    expect(get(store.selectedScriptId)).toBe(2)
  })

  it('asks for approval when switching on a script with unapproved permissions', async () => {
    const s = scriptFixture()
    store.scripts.set([s])
    render(ScriptList)
    await userEvent.click(screen.getByRole('switch', { name: 'Switch on Archive invoices' }))
    expect(get(store.approvalFor)?.id).toBe(1)
    expect(api.setScriptEnabled).not.toHaveBeenCalled()
  })
})

describe('PermissionList', () => {
  it('marks unapproved permissions and their risk', () => {
    render(PermissionList, { props: { catalog: catalogFixture, declared: ['mail.read', 'mail.send'], approved: ['mail.read'] } })
    expect(screen.getByText('High risk')).toBeInTheDocument()
    expect(screen.getAllByText('Not approved')).toHaveLength(1)
  })

  it('lets the user declare permissions but not drop the trigger one', async () => {
    const changes: string[][] = []
    render(PermissionList, {
      props: { catalog: catalogFixture, declared: ['mail.read'], editable: true, locked: 'mail.read' },
      events: { change: (e: CustomEvent<string[]>) => changes.push(e.detail) },
    })
    expect(screen.getByRole('checkbox', { name: /Read mail/ })).toBeDisabled()
    await userEvent.click(screen.getByRole('checkbox', { name: /Send mail/ }))
    expect(changes).toEqual([['mail.read', 'mail.send']])
  })
})

describe('RunSteps', () => {
  it('lists planned changes, checks and errors in order', () => {
    render(RunSteps, {
      props: {
        steps: [
          { kind: 'check', block: 'b1', summary: 'b1', result: true, atMs: 0 },
          { kind: 'planned', function: 'mail.archive', summary: 'mail.archive(message "x")', atMs: 1 },
          { kind: 'denied', function: 'mail.delete', summary: 'mail.delete(1)', error: 'needs mail.delete', atMs: 2 },
        ],
      },
    })
    const items = screen.getAllByRole('listitem')
    expect(items[0]).toHaveTextContent('Condition in b1 held')
    expect(items[1]).toHaveTextContent('would do')
    expect(items[2]).toHaveTextContent('needs mail.delete')
  })
})

describe('AutomationsHost', () => {
  it('asks about a risky call and passes on the answer', async () => {
    api.answerScriptConfirm.mockResolvedValue(undefined)
    render(AutomationsHost)
    store.pendingConfirms.set([{ id: 'q1', scriptId: 1, scriptName: 'Mailer', function: 'mail.send', summary: 'mail.send({"to":["a@b"]})' }])
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('mail.send({"to":["a@b"]})')).toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole('checkbox'))
    await userEvent.click(within(dialog).getByRole('button', { name: 'Allow' }))
    expect(api.answerScriptConfirm).toHaveBeenCalledWith('q1', true, true)
  })

  it('asks about catching up', async () => {
    api.resolveScriptCatchUp.mockResolvedValue(undefined)
    render(AutomationsHost)
    // the host loads the questions already open when it mounts; wait for that.
    await new Promise((resolve) => setTimeout(resolve, 0))
    store.pendingCatchUps.set([{ scriptId: 4, name: 'Rules', count: 12 }])
    const dialog = await screen.findByRole('dialog')
    expect(dialog).toHaveTextContent('12 messages arrived')
    await userEvent.click(within(dialog).getByRole('button', { name: 'Leave them' }))
    expect(api.resolveScriptCatchUp).toHaveBeenCalledWith(4, false)
  })
})

describe('ScriptEditor', () => {
  it('saves only after an edit', async () => {
    const s = scriptFixture({ trigger: { type: 'manual', params: {} }, permissions: [] })
    store.scripts.set([s])
    api.updateScript.mockResolvedValue({ ...s, name: 'Renamed' })
    render(ScriptEditor, { props: { script: s, catalog: catalogFixture } })
    const save = screen.getByRole('button', { name: 'Save' })
    expect(save).toBeDisabled()
    const name = screen.getByRole('textbox', { name: 'Name' })
    await userEvent.clear(name)
    await userEvent.type(name, 'Renamed')
    expect(save).toBeEnabled()
    await userEvent.click(save)
    expect(api.updateScript).toHaveBeenCalledWith(1, expect.objectContaining({ name: 'Renamed' }))
  })

  it('shows a block script as its mount point and the generated Lua', async () => {
    const s = scriptFixture({ kind: 'blocks', source: '', permissions: ['mail.read'], blocks: '{"version":1,"blocks":[{"id":"a","kind":"action","type":"mail.flag"}]}' })
    const { container } = render(ScriptEditor, { props: { script: s, catalog: catalogFixture } })
    expect(container.querySelector('[data-block-editor-mount]')).not.toBeNull()
    expect(await screen.findByText(/The blocks also need: mail.modify/)).toBeInTheDocument()
    expect(api.compileScriptBlocks).toHaveBeenCalledWith('{"version":1,"blocks":[{"id":"a","kind":"action","type":"mail.flag"}]}', 'mail.arrived')
  })

  it('runs a test and shows its steps', async () => {
    const s = scriptFixture({ trigger: { type: 'manual', params: {} }, permissions: [] })
    api.testScript.mockResolvedValue({
      id: 0, scriptId: 1, scriptName: s.name, trigger: 'manual', dry: true, depth: 0, started: '', durationMs: 1,
      status: 'ok', error: '', steps: [{ kind: 'planned', function: 'mail.archive', summary: 'mail.archive(5)', atMs: 0 }],
      changes: 1, cancelled: false, cancelReason: '', logTruncated: false, undoable: false, messages: [],
    })
    render(ScriptEditor, { props: { script: s, catalog: catalogFixture } })
    await userEvent.click(screen.getByRole('button', { name: 'Run a test' }))
    expect(api.testScript).toHaveBeenCalledWith(expect.objectContaining({ name: s.name }), 0)
    expect(await screen.findByText('mail.archive(5)')).toBeInTheDocument()
  })
})
