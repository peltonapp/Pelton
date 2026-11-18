import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import type { BlockCond, ScriptCatalog, ScriptRun } from '../../lib/scripting'
import { needs, permissionMet } from '../../lib/scripting'
import { blockCatalog, catalogFixture, scriptFixture } from '../../test/scriptfixtures'
import { expectAccessible } from '../../test/a11y'
import { onTestedMessage, testableOnMail } from './blocks/matchcond'

const api = vi.hoisted(() => ({
  checkScriptLua: vi.fn(),
  compileScriptBlocks: vi.fn(),
  testScript: vi.fn(),
  testScriptAskingAI: vi.fn(),
  aiProviders: vi.fn(),
  aiScriptOptions: vi.fn(),
  useScriptTemplate: vi.fn(),
  testScriptMatch: vi.fn(),
  updateScriptLibrary: vi.fn(),
  scriptRuns: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

const { default: ScriptEditor } = await import('./ScriptEditor.svelte')
const { default: TemplateGallery } = await import('./TemplateGallery.svelte')
const { default: MatchTester } = await import('./MatchTester.svelte')
const { default: ScriptImportDialog } = await import('./ScriptImportDialog.svelte')
const { default: ScriptList } = await import('./ScriptList.svelte')
const { default: LibraryEditor } = await import('./LibraryEditor.svelte')
const store = await import('../../stores/scripts')
const { prefs } = await import('../../stores/prefs')

const run: ScriptRun = {
  id: 0, scriptId: 1, scriptName: 'x', trigger: 'manual', dry: true, depth: 0, started: '', durationMs: 1,
  status: 'ok', error: '', steps: [], changes: 0, cancelled: false, cancelReason: '', logTruncated: false,
  undoable: false, messages: [], errorLine: 0, errorBlock: '', notReached: [],
}

const aiCatalog: ScriptCatalog = {
  ...catalogFixture,
  permissions: [
    ...catalogFixture.permissions,
    { id: 'ai', labelKey: '', label: 'Send to an AI service', descKey: '', category: 'ai', risk: 'high' },
    { id: 'ai_local', labelKey: '', label: 'Send to a local AI model', descKey: '', category: 'ai', risk: 'medium' },
    { id: 'mail.compose', labelKey: '', label: 'Write drafts', descKey: '', category: 'mail', risk: 'low' },
  ],
  templates: [
    {
      id: 'ai_reply', labelKey: 'scripts.template.ai_reply', descKey: 'scripts.template.ai_reply.desc', category: 'ai',
      kind: 'blocks', trigger: { type: 'manual' }, permissions: ['mail.read', 'ai', 'mail.compose'], ai: true,
      blocks: { version: 1, blocks: [{ id: 'd', kind: 'action', type: 'mail.move' }, { id: 'i', kind: 'if', branches: [{ when: { kind: 'condition', type: 'mail.from' }, body: [] }] }] },
    },
    { id: 'lua_one', labelKey: '', label: 'A Lua one', descKey: '', description: 'Does Lua things.', category: 'mail', kind: 'lua', trigger: { type: 'app.start' }, permissions: [], ai: false, source: 'print(1)', missing: 'no AI provider' },
  ],
}

beforeEach(() => {
  prefs.update((p) => ({ ...p, reduceMotion: true }))
  Object.values(api).forEach((f) => f.mockReset())
  api.checkScriptLua.mockResolvedValue('')
  api.compileScriptBlocks.mockResolvedValue({ lua: '', errors: [], permissions: ['ai', 'mail.compose'], lines: {} })
  api.testScript.mockResolvedValue(run)
  api.testScriptAskingAI.mockResolvedValue(run)
  api.aiProviders.mockResolvedValue([{ id: 'p1', name: 'Home model', kind: 'ollama', baseUrl: 'http://127.0.0.1', model: 'm', models: [], default: true, hasKey: false, local: true }])
  api.aiScriptOptions.mockResolvedValue({ logText: false })
  api.scriptRuns.mockResolvedValue([])
  store.catalog.set(aiCatalog)
  store.scripts.set([])
  store.libraries.set([])
  store.testRun.set(null)
  store.selectedScriptId.set(null)
  store.selectedLibraryId.set(null)
  store.galleryOpen.set(false)
})

describe('permissions', () => {
  it('meets a base permission with a targeted one, and ai with any provider', () => {
    expect(permissionMet('ai', ['ai_local:p1'])).toBe(true)
    expect(permissionMet('ai', ['ai:cloud'])).toBe(true)
    expect(permissionMet('net', ['net:example.org'])).toBe(true)
    expect(permissionMet('mail.read', ['mail.modify'])).toBe(false)
    expect(permissionMet('net:a.org', ['net'])).toBe(false)
    expect(needs({ permission: 'a', permissions: ['a', 'b'] })).toEqual(['a', 'b'])
    expect(needs({ permission: 'a' })).toEqual(['a'])
  })
})

describe('ScriptEditor and AI', () => {
  const aiScript = scriptFixture({
    kind: 'blocks', source: '', trigger: { type: 'manual', params: {} }, permissions: ['ai'],
    blocks: '{"version":1,"blocks":[{"id":"a","kind":"action","type":"ai.draft_reply"}]}',
  })

  it('lets the user pick a provider and counts it as the AI the blocks need', async () => {
    render(ScriptEditor, { props: { script: aiScript, catalog: aiCatalog } })
    expect(await screen.findByText(/The blocks also need: mail.compose$/)).toBeInTheDocument()
    const box = await screen.findByRole('checkbox', { name: 'Home model' })
    await userEvent.click(box)
    expect(screen.queryByText(/The blocks also need: ai/)).not.toBeInTheDocument()
    expect(api.aiScriptOptions).toHaveBeenCalledWith(1)
  })

  it('asks the model in a test run only while the toggle is on', async () => {
    render(ScriptEditor, { props: { script: aiScript, catalog: aiCatalog } })
    await userEvent.click(screen.getByRole('button', { name: 'Run a test' }))
    expect(api.testScript).toHaveBeenCalled()
    expect(api.testScriptAskingAI).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('checkbox', { name: /Ask the model/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Run a test' }))
    expect(api.testScriptAskingAI).toHaveBeenCalledTimes(1)
    expect(api.testScriptAskingAI.mock.calls[0][0]).toBe(aiScript.id)
  })

  it('shows no AI controls for a script without AI', () => {
    render(ScriptEditor, { props: { script: scriptFixture({ trigger: { type: 'manual', params: {} }, permissions: [] }), catalog: aiCatalog } })
    expect(screen.queryByRole('checkbox', { name: /Ask the model/ })).not.toBeInTheDocument()
    expect(screen.queryByText('Home model')).not.toBeInTheDocument()
  })

  it('marks the line the last test run failed on', async () => {
    const s = scriptFixture({ trigger: { type: 'manual', params: {} }, permissions: [], source: 'local a = 1\nerror("boom")\n' })
    store.testRun.set({ ...run, status: 'error', error: '<string>:2: boom', errorLine: 2 })
    const { container } = render(ScriptEditor, { props: { script: s, catalog: aiCatalog } })
    await new Promise((resolve) => setTimeout(resolve, 0))
    const dot = container.querySelector('.cm-lua-error-dot')
    expect(dot?.getAttribute('title')).toBe('<string>:2: boom')
    expect(container.querySelectorAll('.cm-lua-error-line')).toHaveLength(1)
  })

  it('opens the reference beside Lua and inserts an entry', async () => {
    render(ScriptEditor, { props: { script: scriptFixture({ trigger: { type: 'manual', params: {} }, permissions: [] }), catalog: aiCatalog } })
    await userEvent.click(screen.getByRole('button', { name: 'Reference' }))
    const panel = screen.getByRole('complementary', { name: 'Reference' })
    await userEvent.type(within(panel).getByRole('searchbox'), 'mail.send')
    expect(within(panel).getByText('mail.send()')).toBeInTheDocument()
    expectAccessible()
  })
})

describe('TemplateGallery', () => {
  it('lists what each template needs and copies one', async () => {
    api.useScriptTemplate.mockResolvedValue(scriptFixture({ id: 9, name: 'Draft a reply with AI' }))
    render(TemplateGallery, { props: { catalog: aiCatalog } })
    const card = screen.getByRole('heading', { name: 'Draft a reply with AI' }).closest('li')!
    expect(within(card).getByText('Uses AI')).toBeInTheDocument()
    expect(within(card).getByText('Write drafts')).toBeInTheDocument()
    await userEvent.click(within(card).getByRole('button', { name: 'Preview' }))
    expect(within(card).getByRole('list', { name: 'Preview' })).toHaveTextContent('If')
    await userEvent.click(within(card).getByRole('button', { name: 'Use template' }))
    expect(api.useScriptTemplate).toHaveBeenCalledWith('ai_reply')
    expect(get(store.selectedScriptId)).toBe(9)
    const lua = screen.getByRole('heading', { name: 'A Lua one' }).closest('li')!
    expect(within(lua).getByText('Not ready yet: no AI provider')).toBeInTheDocument()
  })
})

describe('MatchTester', () => {
  it('tries a pattern and marks where it matched', async () => {
    api.testScriptMatch.mockResolvedValue({
      scanned: 40, matched: 1, timedOut: false,
      matches: [{ messageId: 3, subject: 'Invoice 7', from: 'a@b.c', date: 1700000000, text: 'Invoice 7 is due', spans: [[0, 9]] }],
    })
    render(MatchTester, { props: { pattern: 'Invoice \\d+' } })
    await userEvent.click(screen.getByRole('button', { name: 'Try it' }))
    expect(api.testScriptMatch).toHaveBeenCalledWith(expect.objectContaining({ pattern: 'Invoice \\d+', field: 'subject', folder: 'inbox', limit: 100 }))
    expect((await screen.findAllByText('1 of 40 messages match.'))[0]).toBeInTheDocument()
    expect(screen.getByText('Invoice 7', { selector: 'mark' })).toBeInTheDocument()
  })

  it('refuses a pattern RE2 cannot run', async () => {
    render(MatchTester, { props: { pattern: '(?=x)' } })
    expect(screen.getByRole('button', { name: 'Try it' })).toBeDisabled()
  })

  it('tests a condition on the message being tried', async () => {
    api.testScriptMatch.mockResolvedValue({ scanned: 2, matched: 0, timedOut: false, matches: [] })
    const when: BlockCond = { kind: 'any', items: [{ kind: 'condition', type: 'mail.from', args: { message: { ref: 'var:item' }, value: { value: 'x' } } }] }
    expect(testableOnMail(when, blockCatalog)).toBe(true)
    expect(testableOnMail({ kind: 'condition', type: 'nope' }, blockCatalog)).toBe(false)
    const tried = onTestedMessage(when, blockCatalog)
    expect(tried.items?.[0].args?.message).toEqual({ ref: 'event.message' })
    render(MatchTester, { props: { when: tried } })
    expect(screen.queryByRole('textbox', { name: 'Regular expression' })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Try it' }))
    expect(api.testScriptMatch).toHaveBeenCalledWith(expect.objectContaining({ pattern: '', when: tried }))
    expect((await screen.findAllByText('0 of 2 messages match.'))[0]).toBeInTheDocument()
  })
})

describe('libraries', () => {
  const lib = { id: 4, name: 'dates', description: 'Date helpers', source: 'return {}', origin: 'user' as const, createdAt: '', updatedAt: 'a' }

  it('lists libraries in their own section and opens one', async () => {
    store.libraries.set([lib])
    render(ScriptList)
    const section = screen.getByRole('list', { name: 'Libraries' })
    await userEvent.click(within(section).getByRole('button', { name: /dates/ }))
    expect(get(store.selectedLibraryId)).toBe(4)
    await userEvent.click(screen.getByRole('button', { name: 'Templates' }))
    expect(get(store.galleryOpen)).toBe(true)
    expect(get(store.selectedLibraryId)).toBeNull()
  })

  it('saves a library and refuses a name require cannot take', async () => {
    store.libraries.set([lib])
    api.updateScriptLibrary.mockResolvedValue({ ...lib, description: 'More', updatedAt: 'b' })
    render(LibraryEditor, { props: { library: lib, catalog: aiCatalog } })
    const name = screen.getByRole('textbox', { name: 'Library name' })
    await userEvent.clear(name)
    await userEvent.type(name, 'Bad Name')
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(name).toHaveAccessibleDescription(/lower case letters/)
    expectAccessible()
    await userEvent.clear(name)
    await userEvent.type(name, 'dates')
    await userEvent.type(screen.getByRole('textbox', { name: 'What this library offers' }), '!')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(api.updateScriptLibrary).toHaveBeenCalledWith(4, { name: 'dates', description: 'Date helpers!', source: 'return {}' })
  })

  it('warns on import when a needed library is missing', () => {
    render(ScriptImportDialog, {
      props: {
        catalog: aiCatalog,
        file: { script: scriptFixture(), library: null, kind: 'lua', text: '', missingLibraries: ['dates'] },
      },
    })
    expect(screen.getByRole('note')).toHaveTextContent('libraries you do not have: dates')
  })

  it('shows a library file as a library', () => {
    render(ScriptImportDialog, { props: { catalog: aiCatalog, file: { script: scriptFixture(), library: lib, kind: 'library', text: '', missingLibraries: [] } } })
    expect(screen.getByText('Import the library dates?')).toBeInTheDocument()
    expect(screen.queryByText('Permissions')).not.toBeInTheDocument()
  })
})
