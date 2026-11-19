// maildrag.ts is dragging mail with the pointer: pick up one message or the
// whole selection from the list and drop it on a folder, or on another space
// to make something out of it. it is pointer based rather than the browser's
// drag and drop, so the dragged stack can be drawn and animated by the app and
// behaves the same in every webview. the stack itself is DragStack.svelte.

import { writable, get } from 'svelte/store'
import type { MessageSummary } from '../lib/types'

/** A drag in progress. */
export interface MailDrag {
  /** The messages being carried. */
  items: MessageSummary[]
  /** Pointer position in client pixels. */
  x: number
  y: number
  /** True while the pointer is over a target that takes these messages. */
  over: boolean
  /** True while Shift is held, which some targets read as a different drop. */
  shift: boolean
  /** What the target under the pointer says a drop would do, or empty. */
  hint: string
}

/** How the messages were dropped. */
export interface MailDropInfo {
  shift: boolean
}

/** What a drop target does with mail. */
export interface MailDropOptions {
  /** Says whether the target takes these messages; omitted means it takes any. */
  accepts?: (items: MessageSummary[]) => boolean
  /** Runs when the messages are dropped on the target. */
  onDrop: (items: MessageSummary[], info: MailDropInfo) => void
  /** Says what a drop here would do, shown with the dragged stack. */
  hint?: (items: MessageSummary[], shift: boolean) => string
}

/** The drag in progress, or null. */
export const mailDrag = writable<MailDrag | null>(null)

const targets = new Map<HTMLElement, MailDropOptions>()
const startDistance = 6
const overClass = 'maildrop-over'

let hovered: HTMLElement | null = null

function targetAt(x: number, y: number, items: MessageSummary[]): HTMLElement | null {
  let el = document.elementFromPoint(x, y) as HTMLElement | null
  while (el) {
    const options = targets.get(el)
    if (options && (!options.accepts || options.accepts(items))) {
      return el
    }
    el = el.parentElement
  }
  return null
}

function setHovered(next: HTMLElement | null): void {
  if (next === hovered) {
    return
  }
  hovered?.classList.remove(overClass)
  next?.classList.add(overClass)
  hovered = next
}

function hintFor(items: MessageSummary[], shift: boolean): string {
  return (hovered && targets.get(hovered)?.hint?.(items, shift)) || ''
}

function onMove(event: PointerEvent): void {
  const drag = get(mailDrag)
  if (!drag) {
    return
  }
  setHovered(targetAt(event.clientX, event.clientY, drag.items))
  mailDrag.set({ ...drag, x: event.clientX, y: event.clientY, over: hovered !== null, shift: event.shiftKey, hint: hintFor(drag.items, event.shiftKey) })
}

function finish(drop: boolean): void {
  const drag = get(mailDrag)
  const target = hovered
  setHovered(null)
  window.removeEventListener('pointermove', onMove, true)
  window.removeEventListener('pointerup', onUp, true)
  window.removeEventListener('keydown', onKey, true)
  window.removeEventListener('keyup', onKey, true)
  document.body.classList.remove('mail-dragging')
  mailDrag.set(null)
  if (drop && drag && target) {
    targets.get(target)?.onDrop(drag.items, { shift: drag.shift })
  }
}

function onUp(event: PointerEvent): void {
  const drag = get(mailDrag)
  if (drag && drag.shift !== event.shiftKey) {
    mailDrag.set({ ...drag, shift: event.shiftKey })
  }
  finish(true)
}

function onKey(event: KeyboardEvent): void {
  if (event.type === 'keydown' && event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    finish(false)
    return
  }
  const drag = get(mailDrag)
  if (event.key === 'Shift' && drag && drag.shift !== event.shiftKey) {
    mailDrag.set({ ...drag, shift: event.shiftKey, hint: hintFor(drag.items, event.shiftKey) })
  }
}

function begin(items: MessageSummary[], x: number, y: number): void {
  mailDrag.set({ items, x, y, over: false, shift: false, hint: '' })
  document.body.classList.add('mail-dragging')
  window.addEventListener('pointermove', onMove, true)
  window.addEventListener('pointerup', onUp, true)
  window.addEventListener('keydown', onKey, true)
  window.addEventListener('keyup', onKey, true)
}

/**
 * Makes an element a handle that mail can be picked up by. `items` is asked
 * for the messages once the pointer has moved far enough to mean a drag, so a
 * plain click stays a click.
 */
export function maildragSource(node: HTMLElement, items: () => MessageSummary[]): { update(next: () => MessageSummary[]): void; destroy(): void } {
  let current = items
  let startX = 0
  let startY = 0
  let armed = false

  function onDown(event: PointerEvent): void {
    if (event.button !== 0 || event.pointerType === 'touch' || event.metaKey || event.ctrlKey || event.shiftKey) {
      return
    }
    armed = true
    startX = event.clientX
    startY = event.clientY
    window.addEventListener('pointermove', onArmedMove, true)
    window.addEventListener('pointerup', disarm, true)
  }

  function disarm(): void {
    armed = false
    window.removeEventListener('pointermove', onArmedMove, true)
    window.removeEventListener('pointerup', disarm, true)
  }

  function onArmedMove(event: PointerEvent): void {
    if (!armed || Math.hypot(event.clientX - startX, event.clientY - startY) < startDistance) {
      return
    }
    disarm()
    const picked = current()
    if (picked.length === 0) {
      return
    }
    // the release that ends the drag would otherwise also click the row.
    window.addEventListener('click', swallowClick, { capture: true, once: true })
    window.addEventListener('pointerup', () => setTimeout(() => window.removeEventListener('click', swallowClick, true), 0), { capture: true, once: true })
    begin(picked, event.clientX, event.clientY)
  }

  function swallowClick(event: MouseEvent): void {
    event.stopPropagation()
    event.preventDefault()
  }

  node.addEventListener('pointerdown', onDown)
  return {
    update(next: () => MessageSummary[]): void {
      current = next
    },
    destroy(): void {
      disarm()
      node.removeEventListener('pointerdown', onDown)
    },
  }
}

/** Makes an element a place mail can be dropped on. */
export function maildrop(node: HTMLElement, options: MailDropOptions): { update(next: MailDropOptions): void; destroy(): void } {
  targets.set(node, options)
  return {
    update(next: MailDropOptions): void {
      targets.set(node, next)
    },
    destroy(): void {
      if (hovered === node) {
        setHovered(null)
      }
      targets.delete(node)
    },
  }
}
