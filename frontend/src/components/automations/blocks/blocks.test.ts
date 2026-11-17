import { describe, expect, it } from 'vitest'
import type { Block, BlockTree, ScriptCatalog, ScriptRun } from '../../../lib/scripting'
import { blockCatalog } from '../../../test/scriptfixtures'
import {
  addBranch,
  addElse,
  duplicate,
  emptyHistory,
  findBlock,
  flatten,
  getCond,
  indent,
  insertBlock,
  insertCond,
  locate,
  moveBlock,
  moveCond,
  moveDown,
  moveUp,
  outdent,
  parseCondKey,
  parseSlotKey,
  record,
  redo,
  removeBlock,
  removeCond,
  rootSlot,
  shiftCond,
  slotKey,
  condKey,
  treeOf,
  undo,
  unwrapCond,
  wrapCond,
  newId,
} from './tree'
import { entriesFor, refInScope, refText, scopeAt, everyEntry } from './scope'
import { neededPermissions, validate } from './validate'
import { runMarks } from './runmarks'
import { re2Problem } from './re2'
import { blockFor, paletteGroups, paletteItems } from './palette'

const t = (k: string) => k
const ctx = { catalog: blockCatalog, trigger: 'mail.arrived', settings: [{ name: 'target', type: 'folder' as const, labelKey: '', label: 'Target' }] }

function act(id: string, type = 'mail.move', args: Block['args'] = { folder: { value: 'Archive' } }): Block {
  return { id, kind: 'action', type, args }
}

function sample(): BlockTree {
  return treeOf([
    act('a'),
    {
      id: 'i',
      kind: 'if',
      branches: [{ when: { kind: 'all', items: [{ kind: 'condition', type: 'mail.from', args: { value: { value: 'x' } } }] }, body: [act('b')] }],
    },
    act('c'),
  ])
}

describe('tree operations', () => {
  it('inserts, removes and finds blocks', () => {
    let tree = insertBlock(sample(), { slot: { parent: 'i', key: 0 }, index: 0 }, act('n'))
    expect(findBlock(tree, 'i')?.branches?.[0].body.map((b) => b.id)).toEqual(['n', 'b'])
    expect(locate(tree, 'b')).toEqual({ slot: { parent: 'i', key: 0 }, index: 1 })
    tree = removeBlock(tree, 'i')
    expect(flatten(tree).map((b) => b.id)).toEqual(['a', 'c'])
  })

  it('leaves the tree it was given alone', () => {
    const tree = sample()
    const before = JSON.stringify(tree)
    insertBlock(tree, { slot: rootSlot, index: 0 }, act('z'))
    moveDown(tree, 'a')
    expect(JSON.stringify(tree)).toBe(before)
  })

  it('moves within a list and into containers, counting the place before the move', () => {
    const tree = sample()
    expect(moveBlock(tree, 'a', { slot: rootSlot, index: 3 })!.blocks.map((b) => b.id)).toEqual(['i', 'c', 'a'])
    expect(moveBlock(tree, 'a', { slot: rootSlot, index: 1 })).toBe(tree)
    const nested = moveBlock(tree, 'c', { slot: { parent: 'i', key: 0 }, index: 1 })!
    expect(findBlock(nested, 'i')?.branches?.[0].body.map((b) => b.id)).toEqual(['b', 'c'])
  })

  it('refuses to move a container into itself', () => {
    const tree = sample()
    expect(moveBlock(tree, 'i', { slot: { parent: 'i', key: 0 }, index: 0 })).toBeNull()
  })

  it('moves up and down past siblings and out of containers', () => {
    const tree = sample()
    expect(moveUp(tree, 'c').blocks.map((b) => b.id)).toEqual(['a', 'c', 'i'])
    expect(moveUp(tree, 'a')).toBe(tree)
    expect(moveUp(tree, 'b').blocks.map((b) => b.id)).toEqual(['a', 'b', 'i', 'c'])
    expect(moveDown(tree, 'b').blocks.map((b) => b.id)).toEqual(['a', 'i', 'b', 'c'])
  })

  it('indents into the container above and outdents after it', () => {
    const tree = sample()
    const inside = indent(tree, 'c')
    expect(findBlock(inside, 'i')?.branches?.[0].body.map((b) => b.id)).toEqual(['b', 'c'])
    expect(indent(tree, 'i')).toBe(tree)
    const withElse = indent(addElse(tree, 'i'), 'c')
    expect(findBlock(withElse, 'i')?.else?.map((b) => b.id)).toEqual(['c'])
    expect(outdent(inside, 'c').blocks.map((b) => b.id)).toEqual(['a', 'i', 'c'])
    expect(outdent(tree, 'a')).toBe(tree)
  })

  it('duplicates with fresh ids and points copied step references at the copy', () => {
    const tree = treeOf([
      {
        id: 'f',
        kind: 'foreach',
        as: 'item',
        list: { value: ['a'] },
        body: [act('d', 'mail.create_draft', {}), act('e', 'notify.toast', { text: { ref: 'step:d.id' } })],
      },
    ])
    const { tree: next, id } = duplicate(tree, 'f')
    expect(next.blocks.map((b) => b.id)).toEqual(['f', id])
    const copy = findBlock(next, id)!
    const [d2, e2] = copy.body!
    expect(new Set([id, d2.id, e2.id, 'f', 'd', 'e']).size).toBe(6)
    expect(e2.args?.text.ref).toBe(`step:${d2.id}.id`)
  })

  it('makes ids that are unused', () => {
    expect(newId(treeOf([act('b2'), act('b3')]))).toBe('b4')
  })

  it('keeps unknown blocks and fields through edits', () => {
    const odd = { id: 'u', kind: 'webhook', url: 'x' } as unknown as Block
    const tree = moveDown(treeOf([odd, act('a')]), 'u')
    expect(tree.blocks[1]).toEqual(odd)
  })

  it('round-trips slot and condition keys', () => {
    expect(parseSlotKey(slotKey({ parent: 'i', key: 2 }))).toEqual({ parent: 'i', key: 2 })
    expect(parseSlotKey(slotKey(rootSlot))).toEqual(rootSlot)
    expect(parseCondKey(condKey({ block: 'i', branch: 1, path: [0, 2] }))).toEqual({ block: 'i', branch: 1, path: [0, 2] })
    expect(parseCondKey(condKey({ block: 'i', branch: 0, path: [] }))).toEqual({ block: 'i', branch: 0, path: [] })
  })

  it('edits condition groups', () => {
    let tree = sample()
    const root = { block: 'i', branch: 0, path: [] }
    tree = insertCond(tree, root, 1, { kind: 'condition', type: 'mail.subject', args: {} })
    expect(getCond(tree, root)?.items?.map((c) => c.type)).toEqual(['mail.from', 'mail.subject'])
    tree = shiftCond(tree, { ...root, path: [1] }, -1)
    expect(getCond(tree, root)?.items?.map((c) => c.type)).toEqual(['mail.subject', 'mail.from'])
    tree = wrapCond(tree, { ...root, path: [1] }, 'not')
    expect(getCond(tree, { ...root, path: [1] })?.kind).toBe('not')
    tree = moveCond(tree, { ...root, path: [0] }, { ...root, path: [1] }, 1)!
    expect(getCond(tree, { ...root, path: [0] })?.items?.map((c) => c.type)).toEqual(['mail.from', 'mail.subject'])
    expect(moveCond(tree, { ...root, path: [0] }, { ...root, path: [0] }, 0)).toBeNull()
    tree = removeCond(tree, { ...root, path: [0, 1] })
    tree = unwrapCond(tree, { ...root, path: [0] })
    expect(getCond(tree, { ...root, path: [0] })?.type).toBe('mail.from')
    expect(getCond(removeCond(tree, root), root)).toEqual({ kind: 'all', items: [] })
  })

  it('wraps a single root condition when a second one comes in', () => {
    const tree = treeOf([{ id: 'i', kind: 'if', branches: [{ when: { kind: 'condition', type: 'mail.from' }, body: [] }] }])
    const root = { block: 'i', branch: 0, path: [] }
    const next = insertCond(tree, root, 1, { kind: 'condition', type: 'mail.subject' })
    expect(getCond(next, root)?.kind).toBe('all')
    expect(getCond(next, root)?.items).toHaveLength(2)
  })

  it('undoes and redoes', () => {
    const t0 = sample()
    let h = emptyHistory()
    const t1 = removeBlock(t0, 'a')
    h = record(h, t0)
    const u = undo(h, t1)!
    expect(u.tree).toBe(t0)
    const r = redo(u.history, u.tree)!
    expect(r.tree).toBe(t1)
    expect(undo(emptyHistory(), t0)).toBeNull()
    expect(redo(r.history, r.tree)).toBeNull()
  })
})

describe('scope', () => {
  it('offers event fields, earlier steps, variables and settings', () => {
    const tree = treeOf([
      act('d', 'mail.create_draft', {}),
      { id: 's', kind: 'set', as: 'count', value: { value: 1 } },
      { id: 'f', kind: 'foreach', as: 'item', list: { value: ['x'] }, body: [act('in')] },
      act('last'),
    ])
    const refs = scopeAt(tree, 'in', ctx).map((e) => e.ref)
    expect(refs).toEqual(expect.arrayContaining(['event.message', 'event.message.subject', 'step:d.id', 'var:count', 'var:item', 'setting:target']))
    const top = scopeAt(tree, 'd', ctx).map((e) => e.ref)
    expect(top).not.toContain('step:d.id')
    expect(top).not.toContain('var:count')
    expect(scopeAt(tree, 'last', ctx).map((e) => e.ref)).not.toContain('var:item')
  })

  it('types a for each variable by the element type of its list', () => {
    const tree = treeOf([{ id: 'f', kind: 'foreach', as: 'who', list: { ref: 'event.message.recipients' }, body: [act('in')] }])
    const scope = scopeAt(tree, 'in', ctx)
    expect(scope.find((e) => e.ref === 'event.message.recipients')?.of).toBe('address')
    expect(scope.find((e) => e.ref === 'var:who')?.type).toBe('address')
    const typed = treeOf([{ id: 'f', kind: 'foreach', as: 'm', list: { ref: 'step:x.items' }, body: [act('in')] }])
    expect(scopeAt(typed, 'in', ctx).find((e) => e.ref === 'var:m')?.type).toBe('')
  })

  it('leaves out steps of other branches of the same if', () => {
    const tree = treeOf([
      {
        id: 'i',
        kind: 'if',
        branches: [{ when: { kind: 'all', items: [] }, body: [act('d', 'mail.create_draft', {})] }],
        else: [act('x')],
      },
      act('after'),
    ])
    expect(scopeAt(tree, 'x', ctx).map((e) => e.ref)).not.toContain('step:d.id')
    expect(scopeAt(tree, 'after', ctx).map((e) => e.ref)).toContain('step:d.id')
  })

  it('checks references and marks one that left scope after a move', () => {
    let tree = treeOf([act('d', 'mail.create_draft', {}), act('e', 'notify.toast', { text: { ref: 'step:d.id' } })])
    expect(validate(tree, ctx)).toEqual([])
    tree = moveUp(tree, 'e')
    expect(validate(tree, ctx)).toEqual([{ block: 'e', code: 'badRef', param: 'text', detail: 'step:d.id' }])
    const scope = scopeAt(tree, 'e', ctx)
    expect(refInScope('event.message.subject', scope, blockCatalog)).toBe(true)
    expect(refInScope('event.message.nope', scope, blockCatalog)).toBe(false)
    expect(refInScope('setting:target', scope, blockCatalog)).toBe(true)
  })

  it('filters entries by what a parameter takes and labels references', () => {
    const scope = scopeAt(treeOf([act('a')]), 'a', ctx)
    expect(entriesFor({ type: 'ref', valueType: 'message' }, scope, blockCatalog).map((e) => e.ref)).toEqual(['event.message'])
    expect(entriesFor({ type: 'string' }, scope, blockCatalog).map((e) => e.ref)).not.toContain('event.message')
    expect(refText('event.message.subject', everyEntry(treeOf([]), ctx), t)).toBe('Message › subject')
  })
})

describe('validate', () => {
  it('finds missing parameters, empty branches and unknown blocks', () => {
    const tree = treeOf([
      act('m', 'mail.move', {}),
      { id: 'i', kind: 'if', branches: [{ when: { kind: 'all', items: [] }, body: [] }] },
      { id: 'n', kind: 'if', branches: [{ when: { kind: 'not', items: [] }, body: [act('k')] }] },
      act('u', 'nope.nope', {}),
      { id: 'f', kind: 'foreach', as: '1bad', body: [] },
      { id: 'w', kind: 'webhook' } as unknown as Block,
    ])
    const codes = validate(tree, ctx).map((i) => `${i.block}:${i.code}`)
    expect(codes).toEqual(['m:required', 'i:emptyGroup', 'i:emptyBranch', 'n:notOne', 'u:unknownBlock', 'f:noVar', 'f:noList', 'f:emptyBody', 'w:unknownBlock'])
  })

  it('checks regular expressions as RE2 and actions limited to triggers', () => {
    const tree = treeOf([act('r', 'text.check', { pattern: { value: '(a)\\1' } }), act('s', 'outgoing.cancel', {})])
    expect(validate(tree, ctx).map((i) => i.code)).toEqual(['badRegex', 'wrongTrigger'])
  })

  it('gathers the permissions blocks need', () => {
    expect(neededPermissions(sample(), blockCatalog, 'mail.arrived')).toEqual(['mail.read', 'mail.modify'])
  })
})

describe('re2', () => {
  it('rejects what RE2 lacks and accepts its own syntax', () => {
    expect(re2Problem('(?=x)')).toBe('scripts.blocks.regex.lookaround')
    expect(re2Problem('(?<!x)y')).toBe('scripts.blocks.regex.lookaround')
    expect(re2Problem('(a)\\1')).toBe('scripts.blocks.regex.backref')
    expect(re2Problem('(ab')).toBe('scripts.blocks.regex.invalid')
    expect(re2Problem('[(]')).toBe('')
    expect(re2Problem('(?i)^invoice\\s+\\d+$')).toBe('')
    expect(re2Problem('(?P<n>a)[[:alpha:]]\\pL')).toBe('')
    expect(re2Problem('[\\1]')).toBe('')
  })
})

describe('run marks', () => {
  const tree = treeOf([
    {
      id: 'i',
      kind: 'if',
      branches: [
        { when: { kind: 'all', items: [] }, body: [act('first')] },
        { when: { kind: 'all', items: [] }, body: [act('yes')] },
      ],
      else: [act('no')],
    },
    act('p'),
    act('e'),
  ])

  it('marks ran, planned, failed and not reached blocks, and the branch taken', () => {
    const run = {
      steps: [
        { kind: 'check', block: 'i', result: false, branch: 0, summary: 'i', atMs: 0 },
        { kind: 'check', block: 'i', result: true, branch: 1, summary: 'i', atMs: 0 },
        { kind: 'read', block: 'yes', summary: 'read message', atMs: 1 },
        { kind: 'planned', block: 'p', summary: 'mail.move', atMs: 2 },
        { kind: 'denied', block: 'e', summary: 'mail.delete', error: 'not allowed', atMs: 3 },
      ],
      notReached: ['first', 'no'],
      errorBlock: '',
      error: '',
    } as unknown as ScriptRun
    const marks = runMarks(tree, run)
    expect(marks.get('i')).toEqual({ status: 'ran', lines: [], taken: [1] })
    expect(marks.get('yes')?.status).toBe('ran')
    expect(marks.get('first')?.status).toBe('skipped')
    expect(marks.get('no')?.status).toBe('skipped')
    expect(marks.get('p')).toEqual({ status: 'planned', lines: ['mail.move'] })
    expect(marks.get('e')).toEqual({ status: 'error', lines: ['mail.delete: not allowed'] })
  })

  it('takes the else when the last branch fails, and marks the block that failed', () => {
    const run = {
      steps: [
        { kind: 'check', block: 'i', result: false, branch: 0, summary: 'i', atMs: 0 },
        { kind: 'check', block: 'i', result: false, branch: 1, summary: 'i', atMs: 0 },
      ],
      notReached: ['first', 'yes', 'e'],
      errorBlock: 'p',
      error: 'boom',
    } as unknown as ScriptRun
    const marks = runMarks(tree, run)
    expect(marks.get('i')?.taken).toEqual([-1])
    expect(marks.get('no')).toBeUndefined()
    expect(marks.get('p')).toEqual({ status: 'error', lines: ['boom'] })
    expect(marks.get('e')?.status).toBe('skipped')
  })
})

describe('palette', () => {
  it('lists every catalog entry and control block, grouped and searchable', () => {
    const items = paletteItems(blockCatalog, 'mail.arrived', t)
    expect(items.find((i) => i.type === 'outgoing.cancel')?.unavailable).not.toBe('')
    const groups = paletteGroups(items, 'move', t)
    expect(groups.flatMap((g) => g.items.map((i) => i.key))).toEqual(['action:mail.move'])
    expect(paletteGroups(items, '', t, 'conditions').every((g) => g.section === 'conditions')).toBe(true)
    const extra: ScriptCatalog = { ...blockCatalog, actions: [...blockCatalog.actions, { id: 'ext.ping', labelKey: '', label: 'Ping', descKey: '', category: 'ext', params: [], lua: 'ext.ping()' }] }
    expect(paletteItems(extra, 'manual', t).some((i) => i.label === 'Ping')).toBe(true)
  })

  it('wraps a condition added among blocks in an if', () => {
    const items = paletteItems(blockCatalog, 'mail.arrived', t)
    const b = blockFor(items.find((i) => i.type === 'mail.from')!, treeOf([]))
    expect(b.kind).toBe('if')
    expect(b.branches?.[0].when).toEqual({ kind: 'all', items: [{ kind: 'condition', type: 'mail.from', args: {} }] })
  })
})

describe('branches', () => {
  it('adds else if branches', () => {
    const tree = addBranch(sample(), 'i')
    expect(findBlock(tree, 'i')?.branches).toHaveLength(2)
  })
})
