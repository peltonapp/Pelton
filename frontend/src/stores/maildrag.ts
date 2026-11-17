// maildrag.ts is dragging mail with the pointer: pick up one message or the
// whole selection from the list and drop it on a folder, or on another space
// to make something out of it. it is pointer based rather than the browser's
// drag and drop, so the dragged stack can be drawn and animated by the app and
// behaves the same in every webview. the stack itself is DragStack.svelte.
// while a drag runs, places that will not take the mail fade back, a closed
// folder held under the pointer opens, scroll areas follow the pointer near
// their edges, and Escape or leaving the window cancels.

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
// a target that will not take what is being carried fades back for the
// length of the drag, so only the places that would take it look droppable.
const refuseClass = 'maildrop-refuse'

let hovered: HTMLElement | null = null

function refuses(options: MailDropOptions, items: MessageSummary[]): boolean {
  return !!options.accepts && !options.accepts(items)
}

function markRefusing(node: HTMLElement, options: MailDropOptions): void {
  const drag = get(mailDrag)
  node.classList.toggle(refuseClass, !!drag && refuses(options, drag.items))
}

// spring-loaded places: resting on one for a moment while dragging opens it,
// such as a collapsed folder showing its subfolders.
const springs = new Map<HTMLElement, () => void>()
const springDelay = 650
let springing: HTMLElement | null = null
let springTimer: ReturnType<typeof setTimeout> | undefined

function springAt(x: number, y: number): void {
  let el = document.elementFromPoint(x, y) as HTMLElement | null
  while (el && !springs.has(el)) {
    el = el.parentElement
  }
  if (el === springing) {
    return
  }
  clearTimeout(springTimer)
  springing = el
  if (el) {
    const target = el
    springTimer = setTimeout(() => {
      if (springing === target) {
        springs.get(target)?.()
      }
    }, springDelay)
  }
}

function stopSpring(): void {
  clearTimeout(springTimer)
  springing = null
}

// scroll areas that follow the pointer near their top or bottom edge while
// dragging, so a folder out of view can still be reached.
const scrollers = new Set<HTMLElement>()
const edgeZone = 40
const maxScrollStep = 14
let scrollFrame = 0
let pointerX = 0
let pointerY = 0

function uiScale(): number {
  return Number(getComputedStyle(document.documentElement).getPropertyValue('--ui-scale')) || 1
}

/**
 * How far to scroll an area this frame for a pointer at y, in css px:
 * negative near the top edge, positive near the bottom, faster the closer it
 * is, and zero outside the edge zones. top, bottom and y are client pixels.
 */
export function edgeScrollStep(y: number, top: number, bottom: number, scale = 1): number {
  const zone = Math.min(edgeZone * scale, (bottom - top) / 3)
  if (zone <= 0 || y < top || y > bottom) {
    return 0
  }
  if (y < top + zone) {
    return -Math.ceil(((top + zone - y) / zone) * maxScrollStep)
  }
  if (y > bottom - zone) {
    return Math.ceil(((y - (bottom - zone)) / zone) * maxScrollStep)
  }
  return 0
}

function scrollStep(): void {
  scrollFrame = 0
  const drag = get(mailDrag)
  if (!drag) {
    return
  }
  const scale = uiScale()
  let moved = false
  for (const area of scrollers) {
    const rect = area.getBoundingClientRect()
    if (pointerX < rect.left || pointerX > rect.right) {
      continue
    }
    const step = edgeScrollStep(pointerY, rect.top, rect.bottom, scale)
    const before = area.scrollTop
    if (step !== 0) {
      area.scrollTop = before + step
      moved = area.scrollTop !== before
    }
  }
  if (moved) {
    setHovered(targetAt(pointerX, pointerY, drag.items))
    mailDrag.set({ ...drag, over: hovered !== null, hint: hintFor(drag.items, drag.shift) })
    scrollFrame = requestAnimationFrame(scrollStep)
  }
}

function followEdges(): void {
  if (scrollers.size > 0 && scrollFrame === 0) {
    scrollFrame = requestAnimationFrame(scrollStep)
  }
}

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
  pointerX = event.clientX
  pointerY = event.clientY
  springAt(event.clientX, event.clientY)
  followEdges()
}

function cancel(): void {
  finish(false)
}

function finish(drop: boolean): void {
  const drag = get(mailDrag)
  const target = hovered
  setHovered(null)
  stopSpring()
  cancelAnimationFrame(scrollFrame)
  scrollFrame = 0
  window.removeEventListener('pointermove', onMove, true)
  window.removeEventListener('pointerup', onUp, true)
  window.removeEventListener('keydown', onKey, true)
  window.removeEventListener('keyup', onKey, true)
  window.removeEventListener('blur', cancel)
  document.body.classList.remove('mail-dragging')
  for (const el of targets.keys()) {
    el.classList.remove(refuseClass)
  }
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
  for (const [el, options] of targets) {
    markRefusing(el, options)
  }
  window.addEventListener('pointermove', onMove, true)
  window.addEventListener('pointerup', onUp, true)
  window.addEventListener('keydown', onKey, true)
  window.addEventListener('keyup', onKey, true)
  window.addEventListener('blur', cancel)
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
  markRefusing(node, options)
  return {
    update(next: MailDropOptions): void {
      targets.set(node, next)
      markRefusing(node, next)
    },
    destroy(): void {
      if (hovered === node) {
        setHovered(null)
      }
      node.classList.remove(refuseClass)
      targets.delete(node)
    },
  }
}

/**
 * Makes an element spring-loaded while mail is dragged: resting the pointer
 * on it for a moment runs open, which shows what it holds (a collapsed
 * folder's subfolders, say) so the drop can go further in.
 */
export function maildragSpring(node: HTMLElement, open: () => void): { update(next: () => void): void; destroy(): void } {
  springs.set(node, open)
  return {
    update(next: () => void): void {
      springs.set(node, next)
    },
    destroy(): void {
      if (springing === node) {
        stopSpring()
      }
      springs.delete(node)
    },
  }
}

/** Makes a scroll area scroll by itself while mail is dragged near its top or bottom edge. */
export function maildragScroll(node: HTMLElement): { destroy(): void } {
  scrollers.add(node)
  return {
    destroy(): void {
      scrollers.delete(node)
    },
  }
}
