// runmarks.ts reads a test run back onto the blocks: which ran, which only
// planned a change, which failed, which the run never got to (the run lists
// those itself), and which branches of an if it went into (each check names
// its branch).

import type { BlockTree, ScriptRun } from '../../../lib/scripting'
import { findBlock } from './tree'

/** How a block fared in the last test run. */
export type MarkStatus = 'ran' | 'planned' | 'error' | 'skipped'

/** A block's result: its status, the summaries of its steps and, for an if, the branches taken (-1 for else). */
export interface RunMark {
  status: MarkStatus
  lines: string[]
  taken?: number[]
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
    if (s.kind === 'check' && s.branch !== undefined) {
      const count = findBlock(tree, s.block)?.branches?.length ?? 0
      const into = s.result ? s.branch : s.branch === count - 1 ? -1 : null
      if (into !== null && !(m.taken ?? []).includes(into)) {
        m.taken = [...(m.taken ?? []), into]
      }
    } else if (s.kind !== 'check') {
      m.lines.push(s.error ? `${s.summary}: ${s.error}` : s.summary)
    }
    marks.set(s.block, m)
  }
  for (const id of run.notReached ?? []) {
    marks.set(id, { status: 'skipped', lines: [] })
  }
  if (run.errorBlock) {
    const m = marks.get(run.errorBlock) ?? { status: 'error' as MarkStatus, lines: [] }
    marks.set(run.errorBlock, { ...m, status: 'error', lines: m.lines.includes(run.error) ? m.lines : [...m.lines, run.error] })
  }
  return marks
}
