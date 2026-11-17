// tree.ts holds the block editor's operations on a block tree as pure
// functions: each takes a tree and returns a new one, so every edit is one
// value the undo history can keep. Blocks are copied whole, never rebuilt
// field by field, so fields this editor does not know survive every edit.

import { blockTreeVersion, type Block, type BlockArg, type BlockCond, type BlockTree } from '../../../lib/scripting'

/** A list of blocks inside the tree: the top level, a for each body, an if branch's body or an if's else. */
export interface Slot {
  /** The block that owns the list, or null for the top level. */
  parent: string | null
  /** 'body' for the top level and a for each, 'else', or the index of an if branch. */
  key: 'body' | 'else' | number
}

/** A position in a slot. */
export interface Place {
  slot: Slot
  index: number
}

/** Where a condition sits: the if block, its branch, and the item indexes from the branch's root condition down. */
export interface CondPath {
  block: string
  branch: number
  path: number[]
}

/** The top level of the tree. */
export const rootSlot: Slot = { parent: null, key: 'body' }

/** A deep copy of a JSON value. */
export function clone<T>(v: T): T {
  return JSON.parse(JSON.stringify(v)) as T
}

/** Whether two slots name the same list. */
export function sameSlot(a: Slot, b: Slot): boolean {
  return a.parent === b.parent && a.key === b.key
}

/** A stable string for a slot, for DOM attributes and keyed lookups. */
export function slotKey(s: Slot): string {
  return `${s.parent ?? ''}/${s.key}`
}

/** Reads a slot key back. */
export function parseSlotKey(key: string): Slot {
  const at = key.lastIndexOf('/')
  const parent = key.slice(0, at) || null
  const k = key.slice(at + 1)
  return { parent, key: k === 'body' || k === 'else' ? k : Number(k) }
}

/** A stable string for a condition path. */
export function condKey(c: CondPath): string {
  return `${c.block}/${c.branch}/${c.path.join('.')}`
}

/** Reads a condition key back. */
export function parseCondKey(key: string): CondPath {
  const [block, branch, path] = key.split('/')
  return { block, branch: Number(branch), path: path ? path.split('.').map(Number) : [] }
}

/** Whether a block kind holds other blocks; unknown kinds are kept closed. */
export function isContainer(b: Block): boolean {
  return b.kind === 'if' || b.kind === 'foreach'
}

/** The slots a block holds, in order. An if's else only counts once it exists. */
export function childSlots(b: Block): Slot[] {
  if (b.kind === 'if') {
    const out: Slot[] = (b.branches ?? []).map((_, i) => ({ parent: b.id, key: i }))
    if (b.else) {
      out.push({ parent: b.id, key: 'else' })
    }
    return out
  }
  if (b.kind === 'foreach') {
    return [{ parent: b.id, key: 'body' }]
  }
  return []
}

/** The blocks of a slot inside one block, or null when it has no such slot. With create, a missing list is added to b. */
function listOf(b: Block, key: Slot['key'], create = false): Block[] | null {
  if (b.kind === 'if') {
    if (key === 'else') {
      return b.else ?? null
    }
    const branch = typeof key === 'number' ? b.branches?.[key] : undefined
    if (!branch) {
      return null
    }
    if (!branch.body && create) {
      branch.body = []
    }
    return branch.body ?? []
  }
  if (b.kind === 'foreach' && key === 'body') {
    if (!b.body && create) {
      b.body = []
    }
    return b.body ?? []
  }
  return null
}

/** Finds a block anywhere in the tree. */
export function findBlock(tree: BlockTree, id: string): Block | null {
  let found: Block | null = null
  walk(tree.blocks, (b) => {
    if (b.id === id) {
      found = b
      return false
    }
    return true
  })
  return found
}

/** Visits every block in document order; returning false stops the walk. */
export function walk(blocks: Block[], visit: (b: Block, depth: number) => boolean, depth = 0): boolean {
  for (const b of blocks) {
    if (!visit(b, depth)) {
      return false
    }
    for (const s of childSlots(b)) {
      if (!walk(listOf(b, s.key) ?? [], visit, depth + 1)) {
        return false
      }
    }
  }
  return true
}

/** Every block in document order, as the keyboard steps through them. */
export function flatten(tree: BlockTree): Block[] {
  const out: Block[] = []
  walk(tree.blocks, (b) => {
    out.push(b)
    return true
  })
  return out
}

/** The blocks of a slot in the tree, or null when the slot does not exist. */
export function slotBlocks(tree: BlockTree, slot: Slot, create = false): Block[] | null {
  if (slot.parent === null) {
    return tree.blocks
  }
  const owner = findBlock(tree, slot.parent)
  return owner ? listOf(owner, slot.key, create) : null
}

/** Where a block sits. */
export function locate(tree: BlockTree, id: string): Place | null {
  const search = (blocks: Block[], slot: Slot): Place | null => {
    for (let i = 0; i < blocks.length; i++) {
      const b = blocks[i]
      if (b.id === id) {
        return { slot, index: i }
      }
      for (const s of childSlots(b)) {
        const hit = search(listOf(b, s.key) ?? [], s)
        if (hit) {
          return hit
        }
      }
    }
    return null
  }
  return search(tree.blocks, rootSlot)
}

/** The ids of a block and everything inside it. */
export function subtreeIds(b: Block): Set<string> {
  const ids = new Set<string>()
  walk([b], (x) => {
    ids.add(x.id)
    return true
  })
  return ids
}

/** The containers around a block, outermost first. */
export function ancestors(tree: BlockTree, id: string): Block[] {
  const out: Block[] = []
  let place = locate(tree, id)
  while (place && place.slot.parent !== null) {
    const owner = findBlock(tree, place.slot.parent)
    if (!owner) {
      break
    }
    out.unshift(owner)
    place = locate(tree, owner.id)
  }
  return out
}

/** A block id not used anywhere in the tree. */
export function newId(tree: BlockTree, taken: Set<string> = new Set()): string {
  const used = new Set(taken)
  walk(tree.blocks, (b) => {
    used.add(b.id)
    return true
  })
  let n = used.size + 1
  while (used.has(`b${n}`)) {
    n++
  }
  return `b${n}`
}

/** Puts a block at a place. A place past the end appends. */
export function insertBlock(tree: BlockTree, place: Place, block: Block): BlockTree {
  const next = clone(tree)
  const list = slotBlocks(next, place.slot, true)
  if (!list) {
    return tree
  }
  list.splice(Math.max(0, Math.min(place.index, list.length)), 0, clone(block))
  return next
}

/** Takes a block and everything inside it out of the tree. */
export function removeBlock(tree: BlockTree, id: string): BlockTree {
  const place = locate(tree, id)
  if (!place) {
    return tree
  }
  const next = clone(tree)
  slotBlocks(next, place.slot)?.splice(place.index, 1)
  return next
}

/** Moves a block to a place given in the tree as it is now. Returns null when the place is inside the block itself. */
export function moveBlock(tree: BlockTree, id: string, to: Place): BlockTree | null {
  const from = locate(tree, id)
  const block = findBlock(tree, id)
  if (!from || !block) {
    return null
  }
  if (to.slot.parent !== null && subtreeIds(block).has(to.slot.parent)) {
    return null
  }
  let index = to.index
  if (sameSlot(from.slot, to.slot)) {
    if (index === from.index || index === from.index + 1) {
      return tree
    }
    if (index > from.index) {
      index--
    }
  }
  return insertBlock(removeBlock(tree, id), { slot: to.slot, index }, block)
}

/** Moves a block one step up: past its previous sibling, or out in front of its container at the top of one. */
export function moveUp(tree: BlockTree, id: string): BlockTree {
  const place = locate(tree, id)
  if (!place) {
    return tree
  }
  if (place.index > 0) {
    return moveBlock(tree, id, { slot: place.slot, index: place.index - 1 }) ?? tree
  }
  if (place.slot.parent === null) {
    return tree
  }
  const outer = locate(tree, place.slot.parent)
  return outer ? (moveBlock(tree, id, outer) ?? tree) : tree
}

/** Moves a block one step down: past its next sibling, or out after its container at the end of one. */
export function moveDown(tree: BlockTree, id: string): BlockTree {
  const place = locate(tree, id)
  const list = place ? slotBlocks(tree, place.slot) : null
  if (!place || !list) {
    return tree
  }
  if (place.index < list.length - 1) {
    return moveBlock(tree, id, { slot: place.slot, index: place.index + 2 }) ?? tree
  }
  if (place.slot.parent === null) {
    return tree
  }
  const outer = locate(tree, place.slot.parent)
  return outer ? (moveBlock(tree, id, { slot: outer.slot, index: outer.index + 1 }) ?? tree) : tree
}

/** Moves a block into the container just above it, at the end of its last list. */
export function indent(tree: BlockTree, id: string): BlockTree {
  const place = locate(tree, id)
  const list = place ? slotBlocks(tree, place.slot) : null
  if (!place || !list || place.index === 0) {
    return tree
  }
  const prev = list[place.index - 1]
  const slots = childSlots(prev)
  const target = slots[slots.length - 1]
  if (!target) {
    return tree
  }
  return moveBlock(tree, id, { slot: target, index: (slotBlocks(tree, target) ?? []).length }) ?? tree
}

/** Moves a block out of its container, to just after it. */
export function outdent(tree: BlockTree, id: string): BlockTree {
  const place = locate(tree, id)
  if (!place || place.slot.parent === null) {
    return tree
  }
  const outer = locate(tree, place.slot.parent)
  return outer ? (moveBlock(tree, id, { slot: outer.slot, index: outer.index + 1 }) ?? tree) : tree
}

/** Whether indent or outdent would change anything, for menus that offer them. */
export function canIndent(tree: BlockTree, id: string): boolean {
  return indent(tree, id) !== tree
}

/** Rewrites the step references of a copied block that point into the copy. */
function remapRefs(v: unknown, ids: Map<string, string>): void {
  if (Array.isArray(v)) {
    v.forEach((x) => remapRefs(x, ids))
    return
  }
  if (v && typeof v === 'object') {
    const o = v as Record<string, unknown>
    if (typeof o.ref === 'string' && o.ref.startsWith('step:')) {
      const [head, ...rest] = o.ref.slice(5).split('.')
      const to = ids.get(head)
      if (to) {
        o.ref = ['step:' + to, ...rest].join('.')
      }
    }
    Object.values(o).forEach((x) => remapRefs(x, ids))
  }
}

/** Copies a block with fresh ids just after it. Returns the new tree and the copy's id. */
export function duplicate(tree: BlockTree, id: string): { tree: BlockTree; id: string } {
  const place = locate(tree, id)
  const block = findBlock(tree, id)
  if (!place || !block) {
    return { tree, id }
  }
  const copy = clone(block)
  const ids = new Map<string, string>()
  const taken = new Set<string>()
  walk([copy], (b) => {
    const fresh = newId(tree, taken)
    taken.add(fresh)
    ids.set(b.id, fresh)
    b.id = fresh
    return true
  })
  remapRefs(copy, ids)
  return { tree: insertBlock(tree, { slot: place.slot, index: place.index + 1 }, copy), id: copy.id }
}

/** Changes one block in place through fn, which edits the copy it is given. */
export function updateBlock(tree: BlockTree, id: string, fn: (b: Block) => void): BlockTree {
  const next = clone(tree)
  const b = findBlock(next, id)
  if (!b) {
    return tree
  }
  fn(b)
  return next
}

/** Sets or clears one argument of a block's args. */
export function setArgIn(args: Record<string, BlockArg> | undefined, name: string, arg: BlockArg | undefined): Record<string, BlockArg> {
  const out = { ...(args ?? {}) }
  if (arg === undefined || (arg.ref === undefined && arg.value === undefined)) {
    delete out[name]
  } else {
    out[name] = arg
  }
  return out
}

// --- if branches -------------------------------------------------------------

/** An empty condition group that holds when all of its items do. */
export function emptyGroup(): BlockCond {
  return { kind: 'all', items: [] }
}

/** Adds an "else if" branch to an if block. */
export function addBranch(tree: BlockTree, id: string): BlockTree {
  return updateBlock(tree, id, (b) => {
    b.branches = [...(b.branches ?? []), { when: emptyGroup(), body: [] }]
  })
}

/** Takes an "else if" branch away with its blocks. The first branch stays. */
export function removeBranch(tree: BlockTree, id: string, index: number): BlockTree {
  if (index === 0) {
    return tree
  }
  return updateBlock(tree, id, (b) => {
    b.branches = (b.branches ?? []).filter((_, i) => i !== index)
  })
}

/** Gives an if block an else. */
export function addElse(tree: BlockTree, id: string): BlockTree {
  return updateBlock(tree, id, (b) => {
    b.else ??= []
  })
}

/** Takes an if block's else away with its blocks. */
export function removeElse(tree: BlockTree, id: string): BlockTree {
  return updateBlock(tree, id, (b) => {
    delete b.else
  })
}

// --- conditions --------------------------------------------------------------

function condAt(root: BlockCond, path: number[]): BlockCond | null {
  let c: BlockCond | undefined = root
  for (const i of path) {
    c = c?.items?.[i]
  }
  return c ?? null
}

/** The condition at a path, or null. */
export function getCond(tree: BlockTree, cp: CondPath): BlockCond | null {
  const when = findBlock(tree, cp.block)?.branches?.[cp.branch]?.when
  return when ? condAt(when, cp.path) : null
}

/** Whether a condition can hold others. */
export function isGroup(c: BlockCond): boolean {
  return c.kind === 'all' || c.kind === 'any' || c.kind === 'not'
}

function editCond(tree: BlockTree, cp: CondPath, fn: (branch: { when: BlockCond }, parent: BlockCond | null, index: number) => void): BlockTree {
  return updateBlock(tree, cp.block, (b) => {
    const branch = b.branches?.[cp.branch]
    if (!branch) {
      return
    }
    const parent = cp.path.length > 0 ? condAt(branch.when, cp.path.slice(0, -1)) : null
    fn(branch, parent, cp.path[cp.path.length - 1] ?? -1)
  })
}

/** Replaces the condition at a path. */
export function setCond(tree: BlockTree, cp: CondPath, cond: BlockCond): BlockTree {
  return editCond(tree, cp, (branch, parent, i) => {
    if (!parent) {
      branch.when = clone(cond)
    } else if (parent.items) {
      parent.items[i] = clone(cond)
    }
  })
}

/** Puts a condition into a group at an index. A single condition at a branch's root is first wrapped in an "all" group. */
export function insertCond(tree: BlockTree, group: CondPath, index: number, cond: BlockCond): BlockTree {
  const target = getCond(tree, group)
  if (!target) {
    return tree
  }
  if (!isGroup(target)) {
    const wrapped = setCond(tree, group, { kind: 'all', items: [target] })
    return insertCond(wrapped, group, index, cond)
  }
  return editCond(tree, group, (branch, parent, i) => {
    const g = parent ? parent.items?.[i] : branch.when
    if (g) {
      g.items = [...(g.items ?? [])]
      g.items.splice(Math.max(0, Math.min(index, g.items.length)), 0, clone(cond))
    }
  })
}

/** Takes a condition out of its group. The root of a branch becomes an empty group instead. */
export function removeCond(tree: BlockTree, cp: CondPath): BlockTree {
  if (cp.path.length === 0) {
    return setCond(tree, cp, emptyGroup())
  }
  return editCond(tree, cp, (_, parent, i) => {
    parent?.items?.splice(i, 1)
  })
}

/** Moves a condition into a group of the same if block or another one. Returns null when the group is inside the condition. */
export function moveCond(tree: BlockTree, from: CondPath, to: CondPath, index: number): BlockTree | null {
  const cond = getCond(tree, from)
  if (!cond || from.path.length === 0) {
    return null
  }
  const sameBranch = from.block === to.block && from.branch === to.branch
  if (sameBranch && from.path.length <= to.path.length && from.path.every((v, i) => to.path[i] === v)) {
    return null
  }
  let toPath = to.path
  let at = index
  if (sameBranch) {
    const parentPath = from.path.slice(0, -1)
    const fromIndex = from.path[from.path.length - 1]
    const samePrefix = parentPath.every((v, i) => toPath[i] === v)
    if (samePrefix && toPath.length > parentPath.length && toPath[parentPath.length] > fromIndex) {
      toPath = [...toPath]
      toPath[parentPath.length]--
    }
    if (toPath.length === parentPath.length && samePrefix) {
      if (at === fromIndex || at === fromIndex + 1) {
        return tree
      }
      if (at > fromIndex) {
        at--
      }
    }
  }
  return insertCond(removeCond(tree, from), { ...to, path: toPath }, at, cond)
}

/** Wraps a condition in an all, any or not group. */
export function wrapCond(tree: BlockTree, cp: CondPath, kind: 'all' | 'any' | 'not'): BlockTree {
  const cond = getCond(tree, cp)
  return cond ? setCond(tree, cp, { kind, items: [cond] }) : tree
}

/** Replaces a group of one item with that item. */
export function unwrapCond(tree: BlockTree, cp: CondPath): BlockTree {
  const cond = getCond(tree, cp)
  if (!cond || !isGroup(cond) || (cond.items?.length ?? 0) !== 1) {
    return tree
  }
  return setCond(tree, cp, cond.items![0])
}

/** Moves a condition one place up or down within its group. */
export function shiftCond(tree: BlockTree, cp: CondPath, by: -1 | 1): BlockTree {
  if (cp.path.length === 0) {
    return tree
  }
  const i = cp.path[cp.path.length - 1]
  const parent = { ...cp, path: cp.path.slice(0, -1) }
  const count = getCond(tree, parent)?.items?.length ?? 0
  const to = i + by
  if (to < 0 || to >= count) {
    return tree
  }
  return moveCond(tree, cp, parent, by > 0 ? to + 1 : to) ?? tree
}

/** Changes a condition's arguments. */
export function updateCond(tree: BlockTree, cp: CondPath, fn: (c: BlockCond) => void): BlockTree {
  const cond = getCond(tree, cp)
  if (!cond) {
    return tree
  }
  const copy = clone(cond)
  fn(copy)
  return setCond(tree, cp, copy)
}

// --- new blocks --------------------------------------------------------------

/** The kinds of control block the palette offers next to actions and conditions. */
export type ControlKind = 'if' | 'foreach' | 'set' | 'stop' | 'lua'

/** The condition groups the palette offers. */
export type GroupKind = 'all' | 'any' | 'not'

/** A variable name not set anywhere in the tree yet. */
export function freeVar(tree: BlockTree, base: string): string {
  const used = new Set<string>()
  walk(tree.blocks, (b) => {
    if (b.as) {
      used.add(b.as)
    }
    return true
  })
  if (!used.has(base)) {
    return base
  }
  let n = 2
  while (used.has(`${base}${n}`)) {
    n++
  }
  return `${base}${n}`
}

/** A new control block. */
export function controlBlock(tree: BlockTree, kind: ControlKind, id = newId(tree)): Block {
  switch (kind) {
    case 'if':
      return { id, kind, branches: [{ when: emptyGroup(), body: [] }] }
    case 'foreach':
      return { id, kind, as: freeVar(tree, 'item'), body: [] }
    case 'set':
      return { id, kind, as: freeVar(tree, 'value') }
    case 'lua':
      return { id, kind, code: '' }
    default:
      return { id, kind }
  }
}

/** A new action block. Arguments start empty; the compiler fills in defaults. */
export function actionBlock(tree: BlockTree, type: string, id = newId(tree)): Block {
  return { id, kind: 'action', type, args: {} }
}

/** A new registered condition. */
export function conditionOf(type: string): BlockCond {
  return { kind: 'condition', type, args: {} }
}

/** An if block guarding one condition, for a condition dropped among blocks. */
export function ifWith(tree: BlockTree, cond: BlockCond, id = newId(tree)): Block {
  return { id, kind: 'if', branches: [{ when: cond.kind === 'condition' ? { kind: 'all', items: [cond] } : cond, body: [] }] }
}

/** A tree of the current format holding these blocks. */
export function treeOf(blocks: Block[]): BlockTree {
  return { version: blockTreeVersion, blocks }
}

// --- undo --------------------------------------------------------------------

/** The editor's undo and redo stacks. */
export interface History {
  past: BlockTree[]
  future: BlockTree[]
}

/** How many edits undo reaches back. */
export const historyLimit = 200

/** An empty history. */
export function emptyHistory(): History {
  return { past: [], future: [] }
}

/** Records the tree as it was before an edit. */
export function record(h: History, before: BlockTree): History {
  return { past: [...h.past, before].slice(-historyLimit), future: [] }
}

/** Steps back one edit, or returns null when there is nothing to undo. */
export function undo(h: History, current: BlockTree): { history: History; tree: BlockTree } | null {
  const prev = h.past[h.past.length - 1]
  if (!prev) {
    return null
  }
  return { history: { past: h.past.slice(0, -1), future: [current, ...h.future] }, tree: prev }
}

/** Steps forward one undone edit, or returns null. */
export function redo(h: History, current: BlockTree): { history: History; tree: BlockTree } | null {
  const next = h.future[0]
  if (!next) {
    return null
  }
  return { history: { past: [...h.past, current], future: h.future.slice(1) }, tree: next }
}
