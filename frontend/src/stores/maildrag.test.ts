import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'
import { edgeScrollStep, mailDrag, maildragSource, maildragSpring, maildrop } from './maildrag'
import type { MessageSummary } from '../lib/types'

const item = { id: 7, subject: 'Hello', accountId: 1, folderId: 1 } as MessageSummary

function pointer(type: string, target: EventTarget, x: number, y: number): void {
  target.dispatchEvent(new MouseEvent(type, { bubbles: true, button: 0, clientX: x, clientY: y }))
}

// startDrag picks the item up from a source the way the pointer does.
function startDrag(): HTMLElement {
  const source = document.createElement('div')
  document.body.appendChild(source)
  maildragSource(source, () => [item])
  pointer('pointerdown', source, 0, 0)
  pointer('pointermove', window, 30, 0)
  return source
}

let under: HTMLElement | null = null

beforeEach(() => {
  under = null
  document.elementFromPoint = () => under
})

afterEach(() => {
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
  document.body.innerHTML = ''
  vi.useRealTimers()
})

describe('edgeScrollStep', () => {
  it('stays still away from the edges', () => {
    expect(edgeScrollStep(300, 0, 600)).toBe(0)
  })

  it('scrolls up near the top and down near the bottom, faster closer in', () => {
    expect(edgeScrollStep(30, 0, 600)).toBeLessThan(0)
    expect(edgeScrollStep(590, 0, 600)).toBeGreaterThan(0)
    expect(Math.abs(edgeScrollStep(2, 0, 600))).toBeGreaterThan(Math.abs(edgeScrollStep(30, 0, 600)))
  })

  it('ignores a pointer outside the area', () => {
    expect(edgeScrollStep(-5, 0, 600)).toBe(0)
    expect(edgeScrollStep(700, 0, 600)).toBe(0)
  })

  it('widens the edge zone with the interface scale', () => {
    expect(edgeScrollStep(60, 0, 600, 1)).toBe(0)
    expect(edgeScrollStep(60, 0, 600, 2)).toBeLessThan(0)
  })
})

describe('drop targets during a drag', () => {
  it('fades the targets that will not take the mail, and only while dragging', () => {
    const yes = document.createElement('div')
    const no = document.createElement('div')
    document.body.append(yes, no)
    maildrop(yes, { onDrop: () => {} })
    maildrop(no, { accepts: () => false, onDrop: () => {} })
    startDrag()
    expect(get(mailDrag)?.items).toEqual([item])
    expect(no.classList.contains('maildrop-refuse')).toBe(true)
    expect(yes.classList.contains('maildrop-refuse')).toBe(false)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(get(mailDrag)).toBeNull()
    expect(no.classList.contains('maildrop-refuse')).toBe(false)
  })

  it('highlights the target under the pointer, says what it does, and drops on release', () => {
    const target = document.createElement('div')
    document.body.append(target)
    const onDrop = vi.fn()
    maildrop(target, { onDrop, hint: () => 'Move to Archive' })
    startDrag()
    under = target
    pointer('pointermove', window, 40, 10)
    expect(target.classList.contains('maildrop-over')).toBe(true)
    expect(get(mailDrag)?.hint).toBe('Move to Archive')
    pointer('pointerup', window, 40, 10)
    expect(onDrop).toHaveBeenCalledWith([item], { shift: false, alt: false })
  })

  it('says when Alt was held through the drop', () => {
    const target = document.createElement('div')
    document.body.append(target)
    const onDrop = vi.fn()
    const hint = vi.fn((_items: unknown, _shift: boolean, alt?: boolean) => (alt ? 'Block time' : 'New event'))
    maildrop(target, { onDrop, hint })
    startDrag()
    under = target
    window.dispatchEvent(new MouseEvent('pointermove', { clientX: 40, clientY: 10, altKey: true }))
    expect(get(mailDrag)?.hint).toBe('Block time')
    window.dispatchEvent(new MouseEvent('pointerup', { clientX: 40, clientY: 10, altKey: true }))
    expect(onDrop).toHaveBeenCalledWith([item], { shift: false, alt: true })
  })

  it('drops nothing when Escape cancels', () => {
    const target = document.createElement('div')
    document.body.append(target)
    const onDrop = vi.fn()
    maildrop(target, { onDrop })
    startDrag()
    under = target
    pointer('pointermove', window, 40, 10)
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    pointer('pointerup', window, 40, 10)
    expect(onDrop).not.toHaveBeenCalled()
    expect(target.classList.contains('maildrop-over')).toBe(false)
  })
})

describe('spring-loaded places', () => {
  it('opens once the pointer rests on it', () => {
    vi.useFakeTimers()
    const folder = document.createElement('div')
    const inner = document.createElement('span')
    folder.append(inner)
    document.body.append(folder)
    const open = vi.fn()
    maildragSpring(folder, open)
    startDrag()
    under = inner
    pointer('pointermove', window, 50, 20)
    vi.advanceTimersByTime(300)
    expect(open).not.toHaveBeenCalled()
    vi.advanceTimersByTime(400)
    expect(open).toHaveBeenCalledOnce()
  })

  it('does not open when the pointer moves on in time', () => {
    vi.useFakeTimers()
    const folder = document.createElement('div')
    document.body.append(folder)
    const open = vi.fn()
    maildragSpring(folder, open)
    startDrag()
    under = folder
    pointer('pointermove', window, 50, 20)
    vi.advanceTimersByTime(300)
    under = null
    pointer('pointermove', window, 50, 80)
    vi.advanceTimersByTime(1000)
    expect(open).not.toHaveBeenCalled()
  })
})
