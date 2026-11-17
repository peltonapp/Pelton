// scope.ts works out which values a block can refer to: the fields of the
// trigger's event (and the fields of their value types), the variables set
// above it, the outputs of steps that ran before it and the script's settings.
// All of it comes from the catalog, so a trigger, action or value type
// registered later brings its own fields along.

import type { Block, BlockTree, ScriptCatalog, ScriptParam } from '../../../lib/scripting'
import { childSlots, slotBlocks, subtreeIds } from './tree'

/** Where a value in scope comes from. */
export type ScopeGroup = 'event' | 'var' | 'step' | 'setting'

/** One value a block can refer to. */
export interface ScopeEntry {
  /** The reference as stored, e.g. "event.message.subject" or "step:b3.id". */
  ref: string
  group: ScopeGroup
  /** A param type or value type id; '' when nothing says. */
  type: string
  /** The path of names shown for it, outermost first. */
  path: { labelKey: string; label: string }[]
  /** The block that sets it, for variables and step outputs. */
  block?: string
}

/** The inputs every scope calculation needs. */
export interface ScopeContext {
  catalog: ScriptCatalog
  trigger: string
  settings: ScriptParam[]
}

function eventEntries(ctx: ScopeContext): ScopeEntry[] {
  const trigger = ctx.catalog.triggers.find((t) => t.id === ctx.trigger)
  const out: ScopeEntry[] = []
  for (const f of trigger?.event ?? []) {
    const head = { labelKey: f.labelKey, label: f.label || f.name }
    out.push({ ref: `event.${f.name}`, group: 'event', type: f.type, path: [head] })
    const vt = ctx.catalog.valueTypes.find((v) => v.id === f.type)
    for (const sub of vt?.fields ?? []) {
      out.push({ ref: `event.${f.name}.${sub.name}`, group: 'event', type: sub.type, path: [head, { labelKey: sub.labelKey, label: sub.label || sub.name }] })
    }
  }
  return out
}

function settingEntries(ctx: ScopeContext): ScopeEntry[] {
  return ctx.settings.map((p) => ({ ref: `setting:${p.name}`, group: 'setting' as const, type: p.type, path: [{ labelKey: p.labelKey, label: p.label || p.name }] }))
}

function stepEntries(b: Block, ctx: ScopeContext): ScopeEntry[] {
  if (b.kind !== 'action') {
    return []
  }
  const def = ctx.catalog.actions.find((a) => a.id === b.type)
  return (def?.outputs ?? []).map((f) => ({
    ref: `step:${b.id}.${f.name}`,
    group: 'step' as const,
    type: f.type,
    path: [
      { labelKey: def!.labelKey, label: def!.label || def!.id },
      { labelKey: f.labelKey, label: f.label || f.name },
    ],
    block: b.id,
  }))
}

function varEntry(name: string, block: string): ScopeEntry {
  return { ref: `var:${name}`, group: 'var', type: '', path: [{ labelKey: '', label: name }], block }
}

/** Adds what a block that ran in full leaves behind: variables it set and step outputs, its own and those of blocks inside it. */
function addRan(b: Block, ctx: ScopeContext, out: ScopeEntry[]): void {
  if (b.kind === 'set' && b.as) {
    out.push(varEntry(b.as, b.id))
  }
  out.push(...stepEntries(b, ctx))
  for (const s of childSlots(b)) {
    for (const inner of slotBlocks({ version: 1, blocks: [b] }, s) ?? []) {
      addRan(inner, ctx, out)
    }
  }
}

/**
 * Everything a block can refer to where it stands: the event and the
 * settings always, then what the blocks before it on its way from the top
 * set. A step inside an earlier sibling's branch counts; one in another
 * branch of an if the block sits in does not, since that branch did not run.
 */
export function scopeAt(tree: BlockTree, id: string, ctx: ScopeContext): ScopeEntry[] {
  const out = [...eventEntries(ctx)]
  const visit = (blocks: Block[]): boolean => {
    for (const b of blocks) {
      if (b.id === id) {
        return true
      }
      if (subtreeIds(b).has(id)) {
        if (b.kind === 'foreach' && b.as) {
          out.push(varEntry(b.as, b.id))
        }
        for (const s of childSlots(b)) {
          const list = slotBlocks({ version: 1, blocks: [b] }, s) ?? []
          if (list.some((x) => subtreeIds(x).has(id))) {
            return visit(list)
          }
        }
        return true
      }
      addRan(b, ctx, out)
    }
    return false
  }
  visit(tree.blocks)
  out.push(...settingEntries(ctx))
  const seen = new Set<string>()
  return out.filter((e) => !seen.has(e.ref) && seen.add(e.ref))
}

/** Every value any block could refer to, for labelling a chip whose block moved. */
export function everyEntry(tree: BlockTree, ctx: ScopeContext): ScopeEntry[] {
  const out = [...eventEntries(ctx)]
  for (const b of tree.blocks) {
    addRan(b, ctx, out)
  }
  const foreachVars = (blocks: Block[]): void => {
    for (const b of blocks) {
      if (b.kind === 'foreach' && b.as) {
        out.push(varEntry(b.as, b.id))
      }
      for (const s of childSlots(b)) {
        foreachVars(slotBlocks({ version: 1, blocks: [b] }, s) ?? [])
      }
    }
  }
  foreachVars(tree.blocks)
  out.push(...settingEntries(ctx))
  return out
}

/** The words a chip shows for an entry, e.g. "Message › Subject". */
export function entryText(e: ScopeEntry, t: (key: string) => string): string {
  return e.path
    .map((p) => {
      const s = p.labelKey ? t(p.labelKey) : ''
      return s && s !== p.labelKey ? s : p.label
    })
    .join(' › ')
}

/** The words a chip shows for a reference, from the entry it names or the reference itself. */
export function refText(ref: string, entries: ScopeEntry[], t: (key: string) => string): string {
  const e = entries.find((x) => x.ref === ref)
  if (e) {
    return entryText(e, t)
  }
  const head = entries.find((x) => ref.startsWith(x.ref + '.'))
  return head ? `${entryText(head, t)} › ${ref.slice(head.ref.length + 1)}` : ref
}

const scalarTypes =new Set(['string', 'text', 'number', 'bool', 'enum', 'regex', 'duration', 'time', 'folder', 'account', 'color', 'address', 'list'])

/**
 * Whether a reference can stand where it is: one of the entries in scope, or
 * a deeper path into an entry whose fields nothing describes (a table, a
 * variable). A variable or step reference with a path only needs its head in
 * scope.
 */
export function refInScope(ref: string, scope: ScopeEntry[], catalog: ScriptCatalog): boolean {
  if (scope.some((e) => e.ref === ref)) {
    return true
  }
  if (ref.startsWith('var:')) {
    const head = 'var:' + ref.slice(4).split('.')[0]
    return scope.some((e) => e.ref === head)
  }
  if (ref.startsWith('step:')) {
    const head = 'step:' + ref.slice(5).split('.')[0]
    return scope.some((e) => e.ref.startsWith(head + '.'))
  }
  return scope.some(
    (e) => ref.startsWith(e.ref + '.') && !scalarTypes.has(e.type) && !catalog.valueTypes.some((v) => v.id === e.type),
  )
}

/**
 * The entries that fit a parameter. A reference-only parameter with a value
 * type wants that type; any other takes plain values, not whole structured
 * ones such as a message.
 */
export function entriesFor(param: Pick<ScriptParam, 'type' | 'valueType'> | null, scope: ScopeEntry[], catalog: ScriptCatalog): ScopeEntry[] {
  if (param?.type === 'ref' && param.valueType) {
    return scope.filter((e) => e.type === param.valueType)
  }
  if (param?.type === 'ref') {
    return scope
  }
  return scope.filter((e) => !catalog.valueTypes.some((v) => v.id === e.type))
}
