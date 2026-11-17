import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, within, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { Block, BlockTree, ScriptRun } from '../../../lib/scripting'
import { blockCatalog } from '../../../test/scriptfixtures'

// jsdom has no ResizeObserver, which bind:clientWidth uses.
globalThis.ResizeObserver ??= class {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}

const api = vi.hoisted(() => ({
  compileScriptBlocks: vi.fn(),
  searchAddresses: vi.fn(),
  testScriptMatch: vi.fn(),
}))

vi.mock('../../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../../lib/api')>()),
  ...api,
}))

const { default: BlockEditor } = await import('./BlockEditor.svelte')
const store = await import('../../../stores/scripts')
const { prefs } = await import('../../../stores/prefs')
const { ruleDraft, ruleChoices, matchText } = await import('./rulefrommail')
const { treeOf } = await import('./tree')

function act(id: string, type: string, args: Block['args'] = {}): Block {
  return { id, kind: 'action', type, args }
}

function setup(tree: BlockTree) {
  const change = vi.fn()
  const permissions = vi.fn()
  const user = userEvent.setup()
  render(BlockEditor, { props: { tree, catalog: blockCatalog, trigger: 'mail.arrived' }, events: { change, permissions } })
  const last = (): BlockTree => change.mock.calls[change.mock.calls.length - 1][0].detail
  return { change, permissions, user, last }
}

function card(name: RegExp): HTMLElement {
  return screen.getByRole('group', { name })
}

beforeEach(() => {
  prefs.update((p) => ({ ...p, reduceMotion: true }))
  api.compileScriptBlocks.mockResolvedValue({ lua: 'local vars, steps = {}, {}\nmail.move(event.message, "A")\n', errors: [], permissions: [] })
  api.searchAddresses.mockResolvedValue([])
  store.testRun.set(null)
  store.selectedScriptId.set(null)
  ruleDraft.set(null)
})

describe('BlockEditor', () => {
  it('teaches the first step and starts with an if', async () => {
    const { user, last } = setup(treeOf([]))
    expect(screen.getByText('Start with a condition or an action')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Start with an If' }))
    expect(last().blocks[0].kind).toBe('if')
    expect(screen.queryByText('Start with a condition or an action')).not.toBeInTheDocument()
  })

  it('builds the palette from the catalog and adds at the caret', async () => {
    const { user, last } = setup(treeOf([act('a', 'notify.toast', { text: { value: 'hi' } })]))
    const palette = screen.getByRole('complementary', { name: 'Blocks to add' })
    expect(within(palette).getByRole('button', { name: 'Check text' })).toBeInTheDocument()
    expect(within(palette).getByRole('button', { name: 'Stop sending' })).toHaveAttribute('aria-disabled', 'true')
    await user.type(within(palette).getByRole('searchbox'), 'move')
    await user.click(within(palette).getByRole('button', { name: 'Move' }))
    expect(last().blocks.map((b) => b.type)).toEqual(['notify.toast', 'mail.move'])
    expect(screen.getByText('1 problem to fix')).toBeInTheDocument()
  })

  it('reports the permissions the blocks need', () => {
    const { permissions } = setup(treeOf([act('a', 'mail.move', { folder: { value: 'A' } })]))
    expect(permissions.mock.calls[permissions.mock.calls.length - 1][0].detail).toEqual(['mail.read', 'mail.modify'])
    expect(screen.getByText('Needs')).toBeInTheDocument()
  })

  it('moves, duplicates, deletes and undoes from the keyboard', async () => {
    const { user, last } = setup(treeOf([act('a', 'notify.toast', { text: { value: '1' } }), act('b', 'mail.move', { folder: { value: 'A' } })]))
    card(/^Move/).focus()
    await user.keyboard('{Alt>}{ArrowUp}{/Alt}')
    expect(last().blocks.map((b) => b.id)).toEqual(['b', 'a'])
    await user.keyboard('{Control>}d{/Control}')
    expect(last().blocks).toHaveLength(3)
    await user.keyboard('{Delete}')
    expect(last().blocks).toHaveLength(2)
    await user.keyboard('{Control>}z{/Control}')
    expect(last().blocks).toHaveLength(3)
    await user.keyboard('{Control>}z{/Control}{Control>}z{/Control}')
    expect(last().blocks.map((b) => b.id)).toEqual(['a', 'b'])
    await user.keyboard('{Control>}{Shift>}z{/Shift}{/Control}')
    expect(last().blocks.map((b) => b.id)).toEqual(['b', 'a'])
  })

  it('nests and un-nests with the keyboard', async () => {
    const tree = treeOf([{ id: 'i', kind: 'if', branches: [{ when: { kind: 'all', items: [] }, body: [] }] }, act('b', 'mail.move', { folder: { value: 'A' } })])
    const { user, last } = setup(tree)
    card(/^Move/).focus()
    await user.keyboard('{Alt>}{ArrowRight}{/Alt}')
    expect(last().blocks[0].branches?.[0].body.map((b) => b.id)).toEqual(['b'])
    card(/^Move/).focus()
    await user.keyboard('{Alt>}{ArrowLeft}{/Alt}')
    expect(last().blocks.map((b) => b.id)).toEqual(['i', 'b'])
  })

  it('adds with "/" at the caret', async () => {
    const { user, last } = setup(treeOf([act('a', 'notify.toast', { text: { value: '1' } })]))
    card(/^Show a message/).focus()
    await user.keyboard('/')
    const menu = await screen.findByRole('combobox', { name: 'Find a block' })
    await user.type(menu, 'for each')
    await user.keyboard('{Enter}')
    expect(last().blocks.map((b) => b.kind)).toEqual(['action', 'foreach'])
  })

  it('picks a reference in scope and marks one that left scope', async () => {
    const { user, last } = setup(treeOf([act('d', 'mail.create_draft'), act('e', 'notify.toast', { text: { value: 'x' } })]))
    const toast = card(/^Show a message/)
    await user.click(within(toast).getByRole('button', { name: 'Use a value for Text' }))
    const options = await screen.findAllByRole('option')
    expect(options.map((o) => o.textContent)).toEqual(expect.arrayContaining([expect.stringContaining('Subject')]))
    const step = options.find((o) => o.textContent?.includes('Write a draft'))!
    await user.pointer({ keys: '[MouseLeft>]', target: step })
    expect(last().blocks[1].args?.text).toEqual({ ref: 'step:d.id' })
    card(/^Show a message/).focus()
    await user.keyboard('{Alt>}{ArrowUp}{/Alt}')
    await waitFor(() => expect(screen.getByText('This value is not available here any more. Pick another one.', { selector: '.problem' })).toBeInTheDocument())
    expect(last().blocks[0].args?.text).toEqual({ ref: 'step:d.id' })
  })

  it('keeps an unknown block intact and movable', async () => {
    const odd = { id: 'w', kind: 'webhook', url: 'https://example.org' } as unknown as Block
    const { user, last } = setup(treeOf([act('a', 'notify.toast', { text: { value: '1' } }), odd]))
    expect(screen.getByText('webhook')).toBeInTheDocument()
    card(/^Unknown block/).focus()
    await user.keyboard('{Alt>}{ArrowUp}{/Alt}')
    expect(last().blocks[0]).toEqual(odd)
  })

  it('shows a test run on the blocks', async () => {
    setup(treeOf([act('a', 'mail.move', { folder: { value: 'A' } })]))
    store.testRun.set({ id: 9, status: 'ok', steps: [{ kind: 'planned', block: 'a', summary: 'mail.move to A', atMs: 1 }] } as unknown as ScriptRun)
    expect(await screen.findByText('Would run')).toBeInTheDocument()
    expect(screen.getByText('mail.move to A')).toBeInTheDocument()
  })

  it('validates regular expressions live', async () => {
    const { user } = setup(treeOf([act('r', 'text.check', { pattern: { value: 'ok' } })]))
    const field = screen.getByRole('textbox', { name: 'Pattern' })
    await user.clear(field)
    await user.type(field, '(?=a)')
    expect(screen.getByText('Lookahead and lookbehind do not work in RE2.')).toBeInTheDocument()
    expect(field).toHaveAttribute('aria-invalid', 'true')
  })

  it('asks for the host a web block needs, until one is declared', async () => {
    const catalog = { ...blockCatalog, permissions: [...blockCatalog.permissions, { id: 'net', labelKey: '', label: 'Web', descKey: '', category: 'web', risk: 'high' as const }], actions: [...blockCatalog.actions, { id: 'web.get', labelKey: '', label: 'Fetch', descKey: '', category: 'web', permission: 'net', params: [], lua: 'web.get()' }] }
    const addhost = vi.fn()
    const user = userEvent.setup()
    const { rerender } = render(BlockEditor, { props: { tree: treeOf([act('w', 'web.get')]), catalog, trigger: 'mail.arrived', declared: ['net'] }, events: { addhost } })
    await user.click(screen.getByRole('button', { name: 'Add the host' }))
    expect(addhost).toHaveBeenCalled()
    await rerender({ declared: ['net', 'net:example.org'] })
    expect(screen.queryByRole('button', { name: 'Add the host' })).not.toBeInTheDocument()
  })

  it('tries a condition group on recent mail from the if block', async () => {
    api.testScriptMatch.mockResolvedValue({ scanned: 5, matched: 2, timedOut: false, matches: [] })
    const when = { kind: 'all' as const, items: [{ kind: 'condition' as const, type: 'mail.subject', args: { op: { value: 'contains' }, value: { value: 'Invoice' } } }] }
    const { user } = setup(treeOf([{ id: 'b1', kind: 'if', branches: [{ when, body: [] }] }]))
    await user.click(screen.getAllByRole('button', { name: 'Try this on recent mail' })[0])
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Try it' }))
    expect(api.testScriptMatch).toHaveBeenCalledWith(expect.objectContaining({ when: expect.objectContaining({ kind: 'all' }) }))
    expect(await within(dialog).findByText('2 of 5 messages match.')).toBeInTheDocument()
  })

  it('shows every permission a block needs', () => {
    const catalog = { ...blockCatalog, actions: [...blockCatalog.actions, { id: 'ai.draft_reply', labelKey: '', label: 'Draft with AI', descKey: '', category: 'ai', permission: 'mail.read', permissions: ['mail.read', 'mail.compose'], params: [], lua: 'x()' }] }
    render(BlockEditor, { props: { tree: treeOf([act('d', 'ai.draft_reply')]), catalog, trigger: 'manual' } })
    const block = screen.getByRole('group', { name: /Draft with AI/ })
    expect(within(block).getByText('Read mail')).toBeInTheDocument()
    expect(within(block).getByText('Write drafts')).toBeInTheDocument()
  })

  it('switches a rule from a message to another match with one click', async () => {
    const choices = ruleChoices(blockCatalog, 'message', { from: 'news@x.org', subject: 'Weekly', to: 'Me <me@y.org>', headers: { 'list-id': '<list.x.org>' } })
    const tree = treeOf([{ id: 'b1', kind: 'if', branches: [{ when: { kind: 'all', items: [choices[0].cond] }, body: [] }] }])
    store.selectedScriptId.set(4)
    ruleDraft.set({ scriptId: 4, block: 'b1', choices })
    const { user, last } = setup(tree)
    const chips = within(screen.getByRole('group', { name: 'Match on' }))
    expect(chips.getByRole('button', { name: /Sender/ })).toHaveAttribute('aria-pressed', 'true')
    await user.click(chips.getByRole('button', { name: /List-Id/ }))
    expect(last().blocks[0].branches?.[0].when.items?.[0]).toEqual({ kind: 'condition', type: 'mail.header', args: { value: { value: 'list.x.org' }, name: { value: 'List-Id' } } })
  })
})

describe('rule from mail', () => {
  it('matches on the first of several values and drops a display name', () => {
    expect(matchText('Ann <ann@x.org>, bob@y.org', { split: ',' })).toBe('ann@x.org')
    expect(matchText('<weekly.x.org>', {})).toBe('weekly.x.org')
    expect(matchText('Re: a, b', {})).toBe('Re: a, b')
    expect(matchText(undefined, {})).toBe('')
  })

  it('offers only conditions the catalog has, in the order the value type hints them', () => {
    const choices = ruleChoices({ ...blockCatalog, conditions: blockCatalog.conditions.filter((c) => c.id !== 'mail.subject') }, 'message', { from: 'a@x.org', subject: 'S', to: 'b@y.org, c@z.org' })
    expect(choices.map((c) => c.key)).toEqual(['from', 'to'])
    expect(choices[1].cond).toEqual({ kind: 'condition', type: 'mail.to', args: { value: { value: 'b@y.org' } } })
    expect(ruleChoices(blockCatalog, 'contact', { from: 'a@x.org' })).toEqual([])
  })
})
