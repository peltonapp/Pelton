// runmarks.ts reads a test run's steps back onto the blocks: which ran,
// which only planned a change, which failed, and which sat in a branch the
// run did not take.

import type { Block, BlockTree, RunStep, ScriptRun } from '../../../lib/scripting'
import { childSlots, slotBlocks, walk } from './tree'

/** How a block fared in the last test run. */
export type MarkStatus = 'ran' | 'planned' | 'error' | 'skipped'

/** A block's result: its status and the summaries of its steps. */
export interface RunMark {
  status: MarkStatus
  lines: string[]
}

/** The branches of an if block the run went into: their indexes, and -1 for the else. */
function takenBranches(b: Block, checks: RunStep[]): Set<number> {
  const taken = new Set<number>()
  const count = b.branches?.length ?? 0
  let seq: boolean[] = []
  for (const c of checks) {
    seq.push(!!c.result)
    if (c.result) {
      taken.add(seq.length - 1)
      seq = []
    } else if (seq.length === count) {
      taken.add(-1)
      seq = []
    }
  }
  return taken
}

/** The marks of every block a run says something about. */
export function runMarks(tree: BlockTree, run: ScriptRun | null): Map<string, RunMark> {
  const marks = new Map<string, RunMark>()
  if (!run) {
    return marks
  }
  for (const s of run.steps) {
    if (!s.block) {
      continue
    }
    const m = marks.get(s.block) ?? { status: 'ran' as MarkStatus, lines: [] }
    const failed = !!s.error || s.kind === 'denied' || s.kind === 'declined'
    if (failed) {
      m.status = 'error'
    } else if (s.kind === 'planned' && m.status !== 'error') {
      m.status = 'planned'
    }
    if (s.kind !== 'check') {
      m.lines.push(s.error ? `${s.summary}: ${s.error}` : s.summary)
    }
    marks.set(s.block, m)
  }
  const skip = (list: Block[]) =>
    walk(list, (x) => {
      if (!marks.has(x.id)) {
        marks.set(x.id, { status: 'skipped', lines: [] })
      }
      return true
    })
  walk(tree.blocks, (b) => {
    if (b.kind !== 'if') {
      return true
    }
    const checks = run.steps.filter((s) => s.kind === 'check' && s.block === b.id)
    if (checks.length === 0) {
      return true
    }
    const taken = takenBranches(b, checks)
    for (const slot of childSlots(b)) {
      const index = slot.key === 'else' ? -1 : (slot.key as number)
      if (!taken.has(index)) {
        skip(slotBlocks({ version: 1, blocks: [b] }, slot) ?? [])
      }
    }
    return true
  })
  return marks
}
