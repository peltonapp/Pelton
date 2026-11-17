// validate.ts finds what is wrong with a block tree before the compiler sees
// it: a required parameter left empty, a reference that is not in scope where
// the block now stands, an empty branch or group, a block or condition the
// catalog does not know. Every check reads the catalog, never a block id.

import type { Block, BlockArg, BlockCond, BlockTree, ScriptCatalog, ScriptParam } from '../../../lib/scripting'
import { re2Problem } from './re2'
import { refInScope, scopeAt, type ScopeContext } from './scope'
import { walk, type CondPath } from './tree'

/** What kind of problem an issue is; each has a locale key scripts.blocks.issue.<code>. */
export type IssueCode =
  | 'required'
  | 'badRef'
  | 'badRegex'
  | 'emptyBranch'
  | 'emptyBody'
  | 'emptyGroup'
  | 'notOne'
  | 'unknownBlock'
  | 'unknownCondition'
  | 'noVar'
  | 'noList'
  | 'wrongTrigger'
  | 'missing'

/** One problem, on a block and maybe on one of its conditions or parameters. */
export interface Issue {
  block: string
  code: IssueCode
  /** The parameter, or "list" and "value" of a for each or set block. */
  param?: string
  cond?: CondPath
  /** Extra text, e.g. the triggers an action works with. */
  detail?: string
}

/** The name a for each or set block writes, as Lua allows it. */
export const varNamePattern = /^[A-Za-z_][A-Za-z0-9_]*$/

/** Whether an argument holds nothing. */
export function isEmptyArg(a: BlockArg | undefined): boolean {
  return !a || (!a.ref && (a.value === undefined || a.value === null || a.value === '' || (Array.isArray(a.value) && a.value.length === 0)))
}

function checkArgs(params: ScriptParam[], args: Record<string, BlockArg> | undefined, at: Omit<Issue, 'code'>, scope: () => ReturnType<typeof scopeAt>, catalog: ScriptCatalog, out: Issue[]): void {
  for (const p of params) {
    const a = args?.[p.name]
    if (a?.ref) {
      if (!refInScope(a.ref, scope(), catalog)) {
        out.push({ ...at, code: 'badRef', param: p.name, detail: a.ref })
      }
      continue
    }
    if (isEmptyArg(a)) {
      if (p.required && p.default === undefined && !p.defaultRef) {
        out.push({ ...at, code: 'required', param: p.name })
      }
      continue
    }
    if (p.type === 'regex' && typeof a?.value === 'string' && re2Problem(a.value)) {
      out.push({ ...at, code: 'badRegex', param: p.name })
    }
  }
}

function checkCond(c: BlockCond, cp: CondPath, ctx: ScopeContext, scope: () => ReturnType<typeof scopeAt>, out: Issue[]): void {
  const at = { block: cp.block, cond: cp }
  if (c.kind === 'condition') {
    const def = ctx.catalog.conditions.find((d) => d.id === c.type)
    if (!def) {
      out.push({ ...at, code: 'unknownCondition', detail: c.type })
      return
    }
    checkArgs(def.params, c.args, at, scope, ctx.catalog, out)
    return
  }
  if (c.kind !== 'all' && c.kind !== 'any' && c.kind !== 'not') {
    out.push({ ...at, code: 'unknownCondition', detail: c.kind })
    return
  }
  const items = c.items ?? []
  if (c.kind === 'not' && items.length !== 1) {
    out.push({ ...at, code: 'notOne' })
  } else if (items.length === 0) {
    out.push({ ...at, code: 'emptyGroup' })
  }
  items.forEach((item, i) => checkCond(item, { ...cp, path: [...cp.path, i] }, ctx, scope, out))
}

function checkBlock(b: Block, tree: BlockTree, ctx: ScopeContext, out: Issue[]): void {
  let cached: ReturnType<typeof scopeAt> | null = null
  const scope = () => (cached ??= scopeAt(tree, b.id, ctx))
  const at = { block: b.id }
  switch (b.kind) {
    case 'action': {
      const def = ctx.catalog.actions.find((a) => a.id === b.type)
      if (!def) {
        out.push({ ...at, code: 'unknownBlock', detail: b.type ?? '' })
        return
      }
      if (def.missing) {
        out.push({ ...at, code: 'missing', detail: def.missing })
      }
      if (def.triggers && def.triggers.length > 0 && !def.triggers.includes(ctx.trigger)) {
        out.push({ ...at, code: 'wrongTrigger', detail: def.triggers.join(', ') })
      }
      checkArgs(def.params, b.args, at, scope, ctx.catalog, out)
      return
    }
    case 'if':
      ;(b.branches ?? []).forEach((br, i) => {
        checkCond(br.when, { block: b.id, branch: i, path: [] }, ctx, scope, out)
        if (!br.body || br.body.length === 0) {
          out.push({ ...at, code: 'emptyBranch' })
        }
      })
      if (b.else && b.else.length === 0) {
        out.push({ ...at, code: 'emptyBranch' })
      }
      return
    case 'foreach':
      if (!b.as || !varNamePattern.test(b.as)) {
        out.push({ ...at, code: 'noVar' })
      }
      if (isEmptyArg(b.list)) {
        out.push({ ...at, code: 'noList', param: 'list' })
      } else if (b.list?.ref && !refInScope(b.list.ref, scope(), ctx.catalog)) {
        out.push({ ...at, code: 'badRef', param: 'list', detail: b.list.ref })
      }
      if (!b.body || b.body.length === 0) {
        out.push({ ...at, code: 'emptyBody' })
      }
      return
    case 'set':
      if (!b.as || !varNamePattern.test(b.as)) {
        out.push({ ...at, code: 'noVar' })
      }
      if (b.value?.ref && !refInScope(b.value.ref, scope(), ctx.catalog)) {
        out.push({ ...at, code: 'badRef', param: 'value', detail: b.value.ref })
      }
      return
    case 'stop':
    case 'lua':
      return
    default:
      out.push({ ...at, code: 'unknownBlock', detail: b.kind })
  }
}

/** Every problem in the tree, in document order. */
export function validate(tree: BlockTree, ctx: ScopeContext): Issue[] {
  const out: Issue[] = []
  walk(tree.blocks, (b) => {
    checkBlock(b, tree, ctx, out)
    return true
  })
  return out
}

/** The permissions the blocks need, from the catalog entries they use, with the trigger's first. */
export function neededPermissions(tree: BlockTree, catalog: ScriptCatalog, trigger: string): string[] {
  const out: string[] = []
  const add = (p: string | undefined) => {
    if (p && !out.includes(p)) {
      out.push(p)
    }
  }
  add(catalog.triggers.find((t) => t.id === trigger)?.permission)
  const conds = (c: BlockCond) => {
    if (c.kind === 'condition') {
      add(catalog.conditions.find((d) => d.id === c.type)?.permission)
    }
    c.items?.forEach(conds)
  }
  walk(tree.blocks, (b) => {
    if (b.kind === 'action') {
      add(catalog.actions.find((a) => a.id === b.type)?.permission)
    }
    b.branches?.forEach((br) => conds(br.when))
    return true
  })
  return out
}
