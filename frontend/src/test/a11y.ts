// a11y.ts is a small check for rendered components: every control has an
// accessible name, and nothing takes a positive tabindex, which would pull it
// out of the reading order.
import { expect } from 'vitest'

const controls = [
  'button',
  'a[href]',
  'input:not([type="hidden"])',
  'select',
  'textarea',
  '[contenteditable="true"]',
  '[role="button"]',
  '[role="checkbox"]',
  '[role="switch"]',
  '[role="tab"]',
  '[role="menuitem"]',
  '[role="menuitemradio"]',
  '[role="menuitemcheckbox"]',
  '[role="option"]',
  '[role="radio"]',
  '[role="slider"]',
  '[role="combobox"]',
  '[role="textbox"]',
].join(', ')

/** Every control without an accessible name and every positive tabindex inside root, described. */
export function accessibilityProblems(root: ParentNode = document.body): string[] {
  const problems: string[] = []
  for (const el of root.querySelectorAll<HTMLElement>('[tabindex]')) {
    if (Number(el.getAttribute('tabindex')) > 0) {
      problems.push(`positive tabindex: ${el.outerHTML.slice(0, 160)}`)
    }
  }
  for (const el of root.querySelectorAll<HTMLElement>(controls)) {
    if (el.closest('[aria-hidden="true"], [hidden], [inert]')) {
      continue
    }
    try {
      expect(el).toHaveAccessibleName()
    } catch {
      problems.push(`no accessible name: ${el.outerHTML.slice(0, 160)}`)
    }
  }
  return problems
}

/** Fails on a control without an accessible name or a positive tabindex inside root. */
export function expectAccessible(root: ParentNode = document.body): void {
  expect(accessibilityProblems(root)).toEqual([])
}
