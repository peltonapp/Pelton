// luamap.ts finds which lines of the generated Lua each block became. The
// compiler writes a fixed shape per block kind and fills action and condition
// templates from the catalog, so the line count of each block follows from
// the tree and the catalog. When the count disagrees with the Lua the map is
// dropped rather than pointing at the wrong line.

import type { Block, BlockCond, BlockTree, ScriptCatalog } from '../../../lib/scripting'

/** The first and last line of a block, counted from 1. */
export interface LineRange {
  from: number
  to: number
}

function newlines(s: string | undefined): number {
  return (s ?? '').split('\n').length - 1
}

function condLines(c: BlockCond, catalog: ScriptCatalog): number {
  if (c.kind === 'condition') {
    return newlines(catalog.conditions.find((d) => d.id === c.type)?.lua)
  }
  return (c.items ?? []).reduce((n, item) => n + condLines(item, catalog), 0)
}

/** The lines of each block in Lua compiled without tracing, or an empty map when they cannot be told. */
export function lineMap(tree: BlockTree, catalog: ScriptCatalog, lua: string): Map<string, LineRange> {
  const map = new Map<string, LineRange>()
  let line = 2
  const blocks = (list: Block[] | undefined): boolean => (list ?? []).every(block)
  const block = (b: Block): boolean => {
    const from = line
    switch (b.kind) {
      case 'action': {
        const def = catalog.actions.find((a) => a.id === b.type)
        if (!def) {
          return false
        }
        line += newlines(def.lua) + 1
        break
      }
      case 'if':
        for (const br of b.branches ?? []) {
          line += condLines(br.when, catalog) + 1
          if (!blocks(br.body)) {
            return false
          }
        }
        if (b.else && b.else.length > 0) {
          line++
          if (!blocks(b.else)) {
            return false
          }
        }
        line++
        break
      case 'foreach':
        line += 2
        if (!blocks(b.body)) {
          return false
        }
        line++
        break
      case 'set':
      case 'stop':
        line++
        break
      case 'lua':
        line += (b.code ?? '').replace(/\n+$/, '').split('\n').length + 2
        break
      default:
        return false
    }
    map.set(b.id, { from, to: line - 1 })
    return true
  }
  if (!blocks(tree.blocks)) {
    return new Map()
  }
  const total = lua.replace(/\n$/, '').split('\n').length
  return total === line - 1 ? map : new Map()
}
