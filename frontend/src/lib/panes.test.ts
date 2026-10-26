import { describe, it, expect, beforeEach } from 'vitest'
import { cyclePane, currentPane, focusPane } from './panes'
import { isFunctionKeyCombo } from './shortcuts'

function layout(withMessage: boolean): void {
  document.body.innerHTML = `
    <aside class="sidebar">
      <div class="row"><button id="inbox">Inbox</button></div>
      <div class="row active"><button id="sent">Sent</button></div>
    </aside>
    <section class="list-col"><div role="listbox" tabindex="0" id="rows"></div></section>
    <section class="detail">${withMessage ? '<div class="scroll" tabindex="-1" id="body"></div>' : ''}</section>`
}

describe('panes', () => {
  beforeEach(() => layout(true))

  it('focuses the selected sidebar row, the list and the reading pane', () => {
    focusPane('sidebar')
    expect(document.activeElement?.id).toBe('sent')
    focusPane('list')
    expect(document.activeElement?.id).toBe('rows')
    focusPane('reader')
    expect(document.activeElement?.id).toBe('body')
    expect(currentPane(document.activeElement)).toBe('reader')
  })

  it('cycles forward and back, wrapping around', () => {
    focusPane('list')
    cyclePane(1)
    expect(currentPane(document.activeElement)).toBe('reader')
    cyclePane(1)
    expect(currentPane(document.activeElement)).toBe('sidebar')
    cyclePane(-1)
    expect(currentPane(document.activeElement)).toBe('reader')
  })

  it('skips the reading pane when no message is open', () => {
    layout(false)
    focusPane('list')
    cyclePane(1)
    expect(currentPane(document.activeElement)).toBe('sidebar')
  })

  it('starts at the list from outside the panes', () => {
    ;(document.activeElement as HTMLElement | null)?.blur()
    cyclePane(1)
    expect(currentPane(document.activeElement)).toBe('list')
  })

  it('treats only F1 to F24 as function keys', () => {
    expect(isFunctionKeyCombo('f6')).toBe(true)
    expect(isFunctionKeyCombo('shift+f6')).toBe(true)
    expect(isFunctionKeyCombo('f')).toBe(false)
    expect(isFunctionKeyCombo('f25')).toBe(false)
  })
})
