// axe.ts runs axe-core over a rendered component. jsdom lays nothing out and
// paints nothing, and a component is rendered on its own rather than in a
// page, so the rules that need either are left out; everything else axe
// checks runs.
import axe from 'axe-core'
import { expect } from 'vitest'

const rulesNeedingLayoutOrPaint = ['color-contrast', 'color-contrast-enhanced', 'link-in-text-block', 'target-size', 'scrollable-region-focusable']

const rulesForWholePages = ['region', 'landmark-one-main', 'page-has-heading-one', 'bypass', 'document-title', 'html-has-lang']

/** Which rules a check leaves out besides those jsdom cannot judge. */
export interface AxeCheck {
  /** Rule ids to skip, for a known finding that is reported and not yet fixed. */
  except?: string[]
}

/** What axe finds wrong inside root, one line per rule and element. */
export async function axeProblems(root: Element = document.body, check: AxeCheck = {}): Promise<string[]> {
  const off = [...rulesNeedingLayoutOrPaint, ...rulesForWholePages, ...(check.except ?? [])]
  const result = await axe.run(root, {
    resultTypes: ['violations'],
    rules: Object.fromEntries(off.map((id) => [id, { enabled: false }])),
  })
  return result.violations.flatMap((v) => v.nodes.map((n) => `${v.id}: ${n.failureSummary?.split('\n')[1]?.trim() ?? v.help} at ${n.html.slice(0, 160)}`))
}

/** Fails on anything axe finds wrong inside root. */
export async function expectAxeClean(root: Element = document.body, check: AxeCheck = {}): Promise<void> {
  expect(await axeProblems(root, check)).toEqual([])
}
