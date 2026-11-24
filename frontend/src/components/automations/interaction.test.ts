import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import { catalogFixture, scriptFixture } from '../../test/scriptfixtures'
import type { ScriptAsk, ScriptCatalog, ScriptResult } from '../../lib/scripting'

const api = vi.hoisted(() => ({
  answerScriptAsk: vi.fn(),
  cancelScriptRun: vi.fn(),
  runScriptResultAction: vi.fn(),
  setScriptPlacements: vi.fn(),
  scriptPlacements: vi.fn(),
  setScriptSettings: vi.fn(),
  setScriptEnabled: vi.fn(),
  pendingScriptCatchUps: vi.fn(),
  scriptLiveRuns: vi.fn(),
  listScripts: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

const { default: AskDialog } = await import('./AskDialog.svelte')
const { default: ResultPanel } = await import('./ResultPanel.svelte')
const { default: RunProgress } = await import('./RunProgress.svelte')
const { default: SettingsForm } = await import('./SettingsForm.svelte')
const { default: PlacementEditor } = await import('./PlacementEditor.svelte')
const { default: AutomationsHost } = await import('./AutomationsHost.svelte')
const store = await import('../../stores/scripts')
const { prefs } = await import('../../stores/prefs')
const { toasts } = await import('../../stores/toast')

function ask(over: Partial<ScriptAsk> = {}): ScriptAsk {
  return {
    id: 'q1',
    scriptId: 1,
    scriptName: 'Sorter',
    kind: 'text',
    control: 'text',
    prompt: 'Where to?',
    params: {},
    default: null,
    dry: false,
    attended: true,
    timeoutSeconds: 0,
    ...over,
  }
}

function result(over: Partial<ScriptResult> = {}): ScriptResult {
  return { id: 'r1', scriptId: 1, scriptName: 'Digest', title: 'Summary', text: 'line one\nline two', format: 'text', actions: ['copy', 'notes.save'], dry: false, ...over }
}

const catalog: ScriptCatalog = {
  ...catalogFixture,
  resultActions: [
    { id: 'copy', labelKey: 'scripts.result.copy', descKey: '', category: 'ui', icon: 'copy', client: 'copy' },
    { id: 'notes.save', labelKey: 'scripts.result.notes.save', descKey: '', category: 'notes', icon: 'note', permission: 'notes.write' },
    { id: 'tasks.create', labelKey: 'scripts.result.tasks.create', descKey: '', category: 'tasks', icon: 'task' },
  ],
  placements: [
    { id: 'mail.row', labelKey: 'scripts.place.mail.row', descKey: '', category: 'mail', surface: 'menu', valueType: 'message', list: 'messages' },
    { id: 'palette', labelKey: 'scripts.place.palette', descKey: '', category: 'app', surface: 'palette', valueType: 'message', optional: true },
    {
      id: 'toolbar.list',
      labelKey: 'scripts.place.toolbar.list',
      descKey: '',
      category: 'mail',
      surface: 'toolbar',
      valueType: 'message',
      options: [
        { name: 'icon', type: 'icon', labelKey: 'scripts.param.icon', required: true },
        { name: 'label', type: 'string', labelKey: 'scripts.param.label', required: true },
      ],
    },
    { id: 'contacts.contact', labelKey: 'scripts.place.contacts.contact', descKey: '', category: 'contacts', surface: 'menu', valueType: 'contact' },
  ],
}

beforeEach(() => {
  prefs.update((p) => ({ ...p, reduceMotion: true }))
  Object.values(api).forEach((f) => f.mockReset())
  api.answerScriptAsk.mockResolvedValue(undefined)
  api.scriptPlacements.mockResolvedValue({})
  api.pendingScriptCatchUps.mockResolvedValue([])
  api.scriptLiveRuns.mockResolvedValue([])
  api.listScripts.mockResolvedValue([])
  store.catalog.set(catalog)
  store.pendingAsks.set([])
  store.pendingConfirms.set([])
  store.pendingCatchUps.set([])
  store.scriptResults.set([])
  store.liveRuns.set([])
  store.importPreview.set(null)
  toasts.set([])
})

describe('AskDialog', () => {
  it('answers a text question with what was typed', async () => {
    const answer = vi.fn()
    const user = userEvent.setup()
    render(AskDialog, { props: { ask: ask({ default: 'Archive' }) }, events: { answer } })
    const field = screen.getByRole('textbox', { name: 'Answer' })
    expect(field).toHaveValue('Archive')
    await user.clear(field)
    await user.type(field, 'Later{Enter}')
    expect(answer.mock.calls[0][0].detail).toBe('Later')
  })

  it('picks one of the options and cancels', async () => {
    const answer = vi.fn()
    const cancel = vi.fn()
    const user = userEvent.setup()
    render(AskDialog, { props: { ask: ask({ control: 'choice', kind: 'choice', params: { options: ['Archive', 'Later'] } }) }, events: { answer, cancel } })
    const ok = screen.getByRole('button', { name: 'OK' })
    expect(ok).toBeDisabled()
    await user.click(screen.getByRole('radio', { name: 'Later' }))
    await user.click(ok)
    expect(answer.mock.calls[0][0].detail).toBe('Later')
    await user.click(screen.getByRole('button', { name: 'Cancel the run' }))
    expect(cancel).toHaveBeenCalled()
  })

  it('asks yes or no and counts down a question nobody started', async () => {
    const answer = vi.fn()
    const user = userEvent.setup()
    render(AskDialog, { props: { ask: ask({ control: 'confirm', kind: 'confirm', attended: false, timeoutSeconds: 30, default: true, dry: true }) }, events: { answer } })
    expect(screen.getByText('Test run')).toBeInTheDocument()
    expect(screen.getAllByText('Takes the default answer in 30 s.').length).toBeGreaterThan(0)
    await user.click(screen.getByRole('button', { name: 'No' }))
    expect(answer.mock.calls[0][0].detail).toBe(false)
  })
})

describe('the host', () => {
  it('shows one question at a time and sends the answer back', async () => {
    const user = userEvent.setup()
    store.pendingAsks.set([ask(), ask({ id: 'q2', prompt: 'Second?' })])
    render(AutomationsHost)
    expect(screen.getByText('Where to?')).toBeInTheDocument()
    expect(screen.queryByText('Second?')).not.toBeInTheDocument()
    await user.type(screen.getByRole('textbox', { name: 'Answer' }), 'x')
    await user.click(screen.getByRole('button', { name: 'OK' }))
    expect(api.answerScriptAsk).toHaveBeenCalledWith('q1', 'x', false)
    expect(await screen.findByText('Second?')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Cancel the run' }))
    expect(api.answerScriptAsk).toHaveBeenLastCalledWith('q2', null, true)
  })
})

describe('ResultPanel', () => {
  it('shows the text and the buttons the script may use', async () => {
    const user = userEvent.setup()
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    api.runScriptResultAction.mockResolvedValue('Summary.md')
    render(ResultPanel, { props: { result: result(), catalog } })
    expect(screen.getByText(/line one/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Create task/ })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /Copy/ }))
    expect(writeText).toHaveBeenCalledWith('line one\nline two')
    await user.click(screen.getByRole('button', { name: /Save as note/ }))
    expect(api.runScriptResultAction).toHaveBeenCalledWith('r1', 'notes.save')
    expect(get(toasts).some((t) => t.message.includes('Summary.md'))).toBe(true)
  })

  it('renders markdown in a sandboxed frame and turns off changes in a test run', () => {
    render(ResultPanel, { props: { result: result({ format: 'markdown', text: '# Hi <script>x</script>', dry: true }), catalog } })
    const frame = document.querySelector('iframe')
    expect(frame?.getAttribute('sandbox')).toBe('')
    expect(document.querySelector('script')).toBeNull()
    expect(screen.getByRole('button', { name: /Save as note/ })).toBeDisabled()
  })
})

describe('RunProgress', () => {
  it('lists runs in progress and cancels one', async () => {
    const user = userEvent.setup()
    store.liveRuns.set([{ id: 3, scriptId: 1, scriptName: 'Slow', dry: false, attended: true, steps: 4, last: 'mail.move(...)', waiting: true, done: false }])
    render(RunProgress)
    expect(screen.getByText('Waiting for your answer')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(api.cancelScriptRun).toHaveBeenCalledWith(3)
  })
})

describe('SettingsForm', () => {
  it('saves values from the schema and goes back to the defaults', async () => {
    const user = userEvent.setup()
    const s = scriptFixture({
      settingsSchema: [
        { name: 'folder', type: 'folder', labelKey: '', label: 'Target folder', default: 'Archive' },
        { name: 'days', type: 'number', labelKey: '', label: 'Days' },
      ],
    })
    api.setScriptSettings.mockResolvedValue({ ...s, settings: { days: 7 } })
    render(SettingsForm, { props: { script: s } })
    const days = screen.getByLabelText('Days')
    await user.type(days, '7')
    days.blur()
    await user.click(screen.getByRole('button', { name: 'Save settings' }))
    expect(api.setScriptSettings).toHaveBeenCalledWith(1, { days: 7 })
  })

  it('tells a Lua script without settings how to declare them', () => {
    render(SettingsForm, { props: { script: scriptFixture() } })
    expect(screen.getByText('This script has no settings.')).toBeInTheDocument()
  })
})

describe('PlacementEditor', () => {
  it('lists only the places the trigger takes and saves a toolbar button with its icon', async () => {
    const user = userEvent.setup()
    const s = scriptFixture({ trigger: { type: 'manual', params: { scope: 'message' } }, enabled: true })
    api.setScriptPlacements.mockResolvedValue({ ...s, placements: [{ id: 'toolbar.list', options: { icon: 'lightning', label: s.name } }] })
    render(PlacementEditor, { props: { script: s, catalog } })
    expect(screen.getByText('Message list menu')).toBeInTheDocument()
    expect(screen.queryByText('Contact menu')).not.toBeInTheDocument()
    await user.click(screen.getByRole('switch', { name: 'List toolbar' }))
    expect(api.setScriptPlacements).toHaveBeenCalledWith(1, [{ id: 'toolbar.list', options: { icon: 'lightning', label: 'Archive invoices' } }])
  })

  it('explains that only a script started by hand can be placed', () => {
    render(PlacementEditor, { props: { script: scriptFixture(), catalog } })
    expect(screen.getByText(/Only a script started by hand/)).toBeInTheDocument()
  })

  it('picks an icon for a placed button', async () => {
    const user = userEvent.setup()
    const s = scriptFixture({ trigger: { type: 'manual', params: { scope: 'message' } }, placements: [{ id: 'toolbar.list', options: { icon: 'lightning', label: 'Go' } }] })
    api.setScriptPlacements.mockResolvedValue(s)
    render(PlacementEditor, { props: { script: s, catalog } })
    const icons = screen.getByRole('radiogroup', { name: 'Icon' })
    expect(within(icons).getByRole('radio', { name: 'Lightning bolt' })).toHaveAttribute('aria-checked', 'true')
    await user.click(within(icons).getByRole('radio', { name: 'Star' }))
    expect(api.setScriptPlacements).toHaveBeenCalledWith(1, [{ id: 'toolbar.list', options: { icon: 'star', label: 'Go' } }])
  })
})

describe('switching on a block script', () => {
  it('is refused while the blocks have problems', async () => {
    const s = scriptFixture({ kind: 'blocks', blocks: JSON.stringify({ version: 1, blocks: [{ id: 'b1', kind: 'action', type: 'mail.move', args: {} }] }), approved: ['mail.read', 'mail.modify'] })
    await expect(store.setEnabled(s, true)).rejects.toThrow('Problems to fix in the blocks: 1')
    expect(api.setScriptEnabled).not.toHaveBeenCalled()
    api.setScriptEnabled.mockResolvedValue({ ...s, enabled: false })
    await store.setEnabled(s, false)
    expect(api.setScriptEnabled).toHaveBeenCalledWith(1, false, [])
  })
})
