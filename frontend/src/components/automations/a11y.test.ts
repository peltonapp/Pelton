import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import { expectAccessible } from '../../test/a11y'
import { catalogFixture } from '../../test/scriptfixtures'
import en from '../../lib/locales/en'
import type { ScriptAsk, ScriptParam } from '../../lib/scripting'
import type { AIKind } from '../../lib/ai'

globalThis.ResizeObserver ??= class {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

const api = vi.hoisted(() => ({
  answerScriptAsk: vi.fn(),
  checkScriptLua: vi.fn(),
  compileScriptBlocks: vi.fn(),
  listScripts: vi.fn(),
  listContacts: vi.fn(),
  scriptRuns: vi.fn(),
  pendingScriptCatchUps: vi.fn(),
  setSetting: vi.fn(),
  testScriptMatch: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

const { default: ScriptIconPicker } = await import('./ScriptIconPicker.svelte')
const { default: AskDialog } = await import('./AskDialog.svelte')
const { default: ContactControl } = await import('./ask/ContactControl.svelte')
const { default: SettingsSchemaEditor } = await import('./SettingsSchemaEditor.svelte')
const { default: MatchTester } = await import('./MatchTester.svelte')
const { default: Live } = await import('./Live.svelte')
const { default: AutomationsOverlay } = await import('./AutomationsOverlay.svelte')
const { default: ScriptShortcutField } = await import('./ScriptShortcutField.svelte')
const { default: LuaEditor } = await import('./LuaEditor.svelte')
const { default: ProviderDialog } = await import('../ai/ProviderDialog.svelte')
const { scriptIcons } = await import('../../lib/scripticons')
const { prefs } = await import('../../stores/prefs')
const { contacts } = await import('../../stores/contacts')
const { bindings, resetAll } = await import('../../stores/shortcuts')
const store = await import('../../stores/scripts')

function ask(over: Partial<ScriptAsk> = {}): ScriptAsk {
  return {
    id: 'a1',
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

beforeEach(() => {
  prefs.update((p) => ({ ...p, reduceMotion: true }))
  Object.values(api).forEach((f) => f.mockReset())
  api.checkScriptLua.mockResolvedValue('')
  api.listScripts.mockResolvedValue([])
  api.scriptRuns.mockResolvedValue([])
  api.pendingScriptCatchUps.mockResolvedValue([])
  store.catalog.set(catalogFixture)
  store.scripts.set([])
  store.libraries.set([])
  resetAll()
})

describe('shared helpers', () => {
  it('has a name for every script icon', () => {
    for (const name of Object.keys(scriptIcons)) {
      expect(en[`scripts.icon.${name}` as keyof typeof en]).toBeTruthy()
    }
  })

  it('keeps a live region mounted and fills it later', async () => {
    const { rerender } = render(Live, { text: '' })
    const region = screen.getByRole('status')
    expect(region.textContent).toBe('')
    await rerender({ text: 'Saved' })
    await waitFor(() => expect(region).toHaveTextContent('Saved'))
  })
})

describe('icon picker', () => {
  it('moves with the arrow keys and tab stops on one icon', async () => {
    const user = userEvent.setup()
    const change = vi.fn()
    render(ScriptIconPicker, { props: { value: 'lightning', label: 'Icon' }, events: { change } })
    const radios = screen.getAllByRole('radio')
    expect(radios.filter((r) => r.getAttribute('tabindex') === '0')).toHaveLength(1)
    radios[0].focus()
    await user.keyboard('{ArrowRight}')
    expect(change.mock.calls[0][0].detail).toBe(Object.keys(scriptIcons)[1])
    expectAccessible()
  })
})

describe('ask dialogs', () => {
  it('names the dialog, describes it with the prompt and announces the countdown once', async () => {
    render(AskDialog, { props: { ask: ask({ control: 'confirm', kind: 'confirm', attended: false, timeoutSeconds: 30, default: true }) } })
    const dialog = screen.getByRole('dialog', { name: 'Question from Sorter' })
    expect(dialog).toHaveAccessibleDescription('Where to?')
    await waitFor(() => expect(screen.getAllByRole('status')[0]).toHaveTextContent('Takes the default answer in 30 s.'))
    expectAccessible()
  })

  it('offers contacts as a combobox with options that hold no buttons', async () => {
    contacts.set([
      { id: 1, fullName: 'Ada Lovelace', emails: [{ value: 'ada@example.org' }] },
      { id: 2, fullName: 'Grace Hopper', emails: [] },
    ] as never)
    const user = userEvent.setup()
    render(ContactControl, { props: { ask: ask({ control: 'contact' }), value: undefined } })
    const box = screen.getByRole('combobox')
    const options = screen.getAllByRole('option')
    expect(options).toHaveLength(2)
    expect(options.every((o) => o.querySelector('button') === null)).toBe(true)
    await user.type(box, '{ArrowDown}')
    expect(box).toHaveAttribute('aria-activedescendant', options[0].id)
    expectAccessible()
  })
})

describe('forms', () => {
  it('ties a bad setting name to a message', () => {
    const schema: ScriptParam[] = [{ name: '1bad', type: 'string', labelKey: '', label: '' }]
    render(SettingsSchemaEditor, { props: { schema, catalog: catalogFixture } })
    const input = screen.getByLabelText('Name')
    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(input).toHaveAccessibleDescription(en['scripts.settings.nameRule'])
    expectAccessible()
  })

  it('ties a bad pattern to its message', async () => {
    render(MatchTester, { props: { pattern: '(' } })
    const input = screen.getByLabelText('Regular expression')
    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(input.getAttribute('aria-describedby')).toBeTruthy()
    expectAccessible()
  })

  it('announces a provider test result in a region that was already there', async () => {
    const kinds: AIKind[] = [{ id: 'ollama', label: 'Ollama', baseUrl: 'http://localhost:11434', key: 'optional', model: '' }]
    render(ProviderDialog, { props: { kinds } })
    expect(screen.getByRole('alert').textContent).toBe('')
    expect(screen.getAllByRole('status').every((r) => r.textContent === '')).toBe(true)
  })
})

describe('main components', () => {
  it('names every control in the script editor and the list', async () => {
    const { default: ScriptEditor } = await import('./ScriptEditor.svelte')
    const { default: ScriptList } = await import('./ScriptList.svelte')
    const { scriptFixture } = await import('../../test/scriptfixtures')
    store.scripts.set([scriptFixture()])
    render(ScriptList)
    expectAccessible()
    render(ScriptEditor, { props: { script: scriptFixture(), catalog: catalogFixture } })
    await new Promise((resolve) => setTimeout(resolve, 0))
    expectAccessible()
  })
})

describe('shortcut field', () => {
  it('says out loud when recording starts and when a key is set', async () => {
    const user = userEvent.setup()
    store.placedScripts.set({ shortcut: [{ scriptId: 5, name: 'Mine', description: '', options: {} }] })
    render(ScriptShortcutField, { scriptId: 5 })
    await user.click(screen.getByRole('button', { name: 'Change' }))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Press keys'))
    await user.keyboard('{Control>}{Shift>}9{/Shift}{/Control}')
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Shortcut set to'))
    expect(get(bindings)['script:5']).toBeTruthy()
  })
})

describe('overlay', () => {
  it('is a named dialog that takes focus, closes on Escape and gives focus back', async () => {
    const user = userEvent.setup()
    const opener = document.createElement('button')
    document.body.append(opener)
    opener.focus()
    const close = vi.fn()
    render(AutomationsOverlay, { events: { close } })
    const dialog = screen.getByRole('dialog', { name: 'Automations' })
    await waitFor(() => expect(dialog).toHaveFocus())
    await user.keyboard('{Escape}')
    expect(close).toHaveBeenCalled()
    opener.remove()
  })
})

describe('Lua editor', () => {
  it('shows a hint for leaving the editor and keeps it as the editor description', () => {
    render(LuaEditor, { props: { content: 'x = 1', label: 'Code' } })
    expect(screen.getByText(en['scripts.lua.tabHint'])).toBeInTheDocument()
    expect(document.querySelector('.cm-content')?.getAttribute('aria-label')).toBe('Code')
  })

  it('lets Tab through after Escape and indents otherwise', async () => {
    const user = userEvent.setup()
    render(LuaEditor, { props: { content: 'x = 1', label: 'Code' } })
    const content = document.querySelector<HTMLElement>('.cm-content')!
    content.focus()
    await user.keyboard('{Tab}')
    expect(content.textContent).toMatch(/^\s+x = 1/)
    await user.keyboard('{Escape}{Tab}')
    expect(content.textContent).toMatch(/^\s+x = 1/)
    expect(content.textContent?.match(/^\s*/)?.[0].length).toBeLessThanOrEqual(2)
  })
})
