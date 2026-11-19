import { afterEach, describe, expect, it } from 'vitest'
import { tip } from './tip'

function description(el: Element): string {
  return (el.getAttribute('aria-describedby') ?? '')
    .split(' ')
    .map((id) => document.getElementById(id)?.textContent ?? '')
    .join(' ')
    .trim()
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('tip for assistive tech', () => {
  it('names an icon-only button and describes its shortcut', () => {
    const button = document.createElement('button')
    document.body.appendChild(button)
    tip(button, { text: 'Search', shortcut: '⌘K' })
    expect(button.getAttribute('aria-label')).toBe('Search')
    expect(description(button)).toBe('⌘K')
  })

  it('does not repeat a name the button already has', () => {
    const button = document.createElement('button')
    button.setAttribute('aria-label', 'Close')
    document.body.appendChild(button)
    tip(button, { text: 'Close  ⌘W' })
    expect(button.getAttribute('aria-label')).toBe('Close')
    expect(description(button)).toBe('⌘W')
  })

  it('adds nothing when the tip only repeats the name', () => {
    const button = document.createElement('button')
    button.textContent = 'Archive'
    document.body.appendChild(button)
    tip(button, { text: 'Archive' })
    expect(button.hasAttribute('aria-describedby')).toBe(false)
    expect(button.hasAttribute('aria-label')).toBe(false)
  })

  it('follows an update and cleans up on destroy', () => {
    const button = document.createElement('button')
    button.setAttribute('aria-label', 'Back')
    document.body.appendChild(button)
    const action = tip(button, { text: 'Back', shortcut: '⌘[' })
    action.update({ text: 'Back', shortcut: '⌥←' })
    expect(description(button)).toBe('⌥←')
    action.destroy()
    expect(description(button)).toBe('')
  })
})
