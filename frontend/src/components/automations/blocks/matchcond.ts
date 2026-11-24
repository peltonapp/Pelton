// matchcond.ts prepares a block condition for the match tester, which tries
// it on real messages: only conditions about a message can be tried, and
// whatever message they name in the script (the event's, a loop variable's)
// becomes the message being tried.

import type { BlockCond, ScriptCatalog } from '../../../lib/scripting'

function leaves(c: BlockCond): BlockCond[] {
  return c.kind === 'condition' ? [c] : (c.items ?? []).flatMap(leaves)
}

/** Whether every condition in c tests a message, so it can be tried on mail. */
export function testableOnMail(c: BlockCond, catalog: ScriptCatalog): boolean {
  const all = leaves(c)
  return (
    all.length > 0 &&
    all.every((leaf) => catalog.conditions.find((d) => d.id === leaf.type)?.params.some((p) => p.type === 'ref' && p.valueType === 'message'))
  )
}

/** c with every message parameter pointing at the message being tried. */
export function onTestedMessage(c: BlockCond, catalog: ScriptCatalog): BlockCond {
  if (c.kind !== 'condition') {
    return { ...c, items: (c.items ?? []).map((i) => onTestedMessage(i, catalog)) }
  }
  const def = catalog.conditions.find((d) => d.id === c.type)
  const args = { ...(c.args ?? {}) }
  for (const p of def?.params ?? []) {
    if (p.type === 'ref' && p.valueType === 'message') {
      args[p.name] = { ref: 'event.message' }
    }
  }
  return { ...c, args }
}
