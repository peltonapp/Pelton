// smoothcaret.ts draws the text caret as an element of its own, so it glides
// from one place to the next instead of jumping there, the way word processors
// do. one caret serves the whole window: it follows whichever text field,
// text area or rich editor has focus, and that field's native caret is hidden
// for as long as it does.

import { get } from 'svelte/store'
import { prefs } from '../stores/prefs'

// how quickly the caret closes the distance to where it should be: after this
// many milliseconds it has covered about two thirds of the way.
const followMs = 32
const idleMs = 520
const recheckMs = 200
const settledPx = 0.15
const textTypes = new Set(['text', 'search', 'url', 'tel', 'password'])

// the styles that decide where text sits inside a field, copied onto the
// mirror that measures it.
const mirrored = [
  'boxSizing',
  'borderTopWidth',
  'borderRightWidth',
  'borderBottomWidth',
  'borderLeftWidth',
  'paddingTop',
  'paddingRight',
  'paddingBottom',
  'paddingLeft',
  'fontFamily',
  'fontSize',
  'fontWeight',
  'fontStyle',
  'fontVariant',
  'fontStretch',
  'fontFeatureSettings',
  'letterSpacing',
  'wordSpacing',
  'lineHeight',
  'textAlign',
  'textIndent',
  'textTransform',
  'tabSize',
  'direction',
] as const

interface Spot {
  x: number
  y: number
  h: number
}

type Field = HTMLInputElement | HTMLTextAreaElement

let caret: HTMLDivElement | null = null
let mirror: HTMLDivElement | null = null
let owner: HTMLElement | null = null
let target: Spot | null = null
let shown: Spot | null = null
let frame = 0
let last = 0
let idle: ReturnType<typeof setTimeout> | undefined
let queued = false

function uiScale(): number {
  return Number(getComputedStyle(document.documentElement).getPropertyValue('--ui-scale')) || 1
}

function isField(el: Element): el is Field {
  if (el instanceof HTMLTextAreaElement) {
    return true
  }
  return el instanceof HTMLInputElement && textTypes.has(el.type)
}

// editable finds what the caret should follow for a focused element, or null
// when it is not a text surface this caret can measure. editors that draw a
// cursor of their own (the vim editor) keep it.
function editable(el: Element | null): HTMLElement | null {
  if (!(el instanceof HTMLElement) || el.closest('.cm-editor')) {
    return null
  }
  if (isField(el)) {
    return el.readOnly || el.disabled ? null : el
  }
  return el.isContentEditable ? el : null
}

// fieldSpot measures the caret of an input or text area by laying its text out
// again in a hidden element with the same metrics.
function fieldSpot(el: Field, scale: number): Spot | null {
  const at = el.selectionStart
  if (at === null || at !== el.selectionEnd) {
    return null
  }
  if (!mirror) {
    mirror = document.createElement('div')
    mirror.setAttribute('aria-hidden', 'true')
    mirror.className = 'smooth-caret-mirror'
    document.body.appendChild(mirror)
  }
  const style = getComputedStyle(el)
  for (const prop of mirrored) {
    mirror.style[prop] = style[prop]
  }
  const area = el instanceof HTMLTextAreaElement
  mirror.style.whiteSpace = area ? 'pre-wrap' : 'pre'
  mirror.style.overflowWrap = area ? 'break-word' : 'normal'
  mirror.style.width = area ? `${el.clientWidth + parseFloat(style.borderLeftWidth) + parseFloat(style.borderRightWidth)}px` : 'auto'
  const value = el instanceof HTMLInputElement && el.type === 'password' ? '•'.repeat(el.value.length) : el.value
  mirror.textContent = value.slice(0, at)
  const mark = document.createElement('span')
  mark.textContent = value.slice(at, at + 1) || '.'
  mirror.appendChild(mark)

  const fontSize = parseFloat(style.fontSize) || 14
  const line = parseFloat(style.lineHeight) || fontSize * 1.25
  const box = el.getBoundingClientRect()
  let top = mark.offsetTop - el.scrollTop
  if (!area) {
    // a one-line field centres its text, whatever its padding says.
    top = (el.offsetHeight - line) / 2
  }
  return {
    x: box.left + (mark.offsetLeft - el.scrollLeft) * scale,
    y: box.top + top * scale,
    h: line * scale,
  }
}

// richSpot measures the caret of a rich editor from the selection itself.
function richSpot(el: HTMLElement): Spot | null {
  const selection = window.getSelection()
  if (!selection || selection.rangeCount === 0 || !selection.isCollapsed || !el.contains(selection.anchorNode)) {
    return null
  }
  const range = selection.getRangeAt(0).cloneRange()
  let rect: DOMRect | undefined = range.getClientRects()[0]
  if (!rect) {
    // an empty line has no rect of its own; the element the caret is in does.
    const node = range.startContainer
    const inside = node instanceof Element ? (node.childNodes[range.startOffset] ?? node) : node.parentElement
    const base = inside instanceof Element ? inside : node.parentElement
    if (!base) {
      return null
    }
    const box = base.getBoundingClientRect()
    const style = getComputedStyle(base)
    const line = parseFloat(style.lineHeight) || (parseFloat(style.fontSize) || 14) * 1.25 * uiScale()
    const rtl = style.direction === 'rtl'
    const pad = parseFloat(rtl ? style.paddingRight : style.paddingLeft) || 0
    return { x: rtl ? box.right - pad : box.left + pad, y: box.top + (parseFloat(style.paddingTop) || 0), h: Math.min(line, box.height || line) }
  }
  return { x: rect.left, y: rect.top, h: rect.height }
}

// visible says whether a spot is inside the field and every scrolling box
// around it, so the caret does not show over what the field is clipped by.
function visible(el: HTMLElement, spot: Spot): boolean {
  const middle = spot.y + spot.h / 2
  for (let node: HTMLElement | null = el; node && node !== document.body; node = node.parentElement) {
    if (node !== el && getComputedStyle(node).overflow === 'visible') {
      continue
    }
    const box = node.getBoundingClientRect()
    if (spot.x < box.left - 2 || spot.x > box.right + 2 || middle < box.top || middle > box.bottom) {
      return false
    }
  }
  // something laid over the field (settings, a dialog) hides its caret too,
  // even though the field underneath still has focus.
  const own = el.getBoundingClientRect()
  const x = Math.min(Math.max(spot.x, own.left + 1), own.right - 1)
  // tooltips let clicks through, so the hit test below does not see them.
  for (const tip of document.querySelectorAll('.tip, [role="tooltip"]')) {
    const box = tip.getBoundingClientRect()
    if (spot.x >= box.left && spot.x <= box.right && spot.y + spot.h >= box.top && spot.y <= box.bottom) {
      return false
    }
  }
  const top = document.elementFromPoint(x, middle)
  return top === null || el.contains(top)
}

function hide(): void {
  // a blink still waiting to start would bring the caret back where it was.
  clearTimeout(idle)
  caret?.classList.remove('shown', 'blink')
  target = null
  shown = null
  cancelAnimationFrame(frame)
  frame = 0
}

function release(): void {
  owner?.removeAttribute('data-smooth-caret')
  owner = null
  hide()
}

function paint(spot: Spot, scale: number): void {
  if (!caret) {
    return
  }
  caret.style.transform = `translate(${spot.x / scale}px, ${spot.y / scale}px)`
  caret.style.height = `${spot.h / scale}px`
}

// step moves the caret a share of the remaining way each frame. the share
// depends on the time passed, so the glide is the same at any frame rate and
// a new position mid-flight simply bends the path.
function step(now: number): void {
  frame = 0
  if (!target || !shown) {
    return
  }
  const share = 1 - Math.exp(-(now - last) / followMs)
  last = now
  shown = {
    x: shown.x + (target.x - shown.x) * share,
    y: shown.y + (target.y - shown.y) * share,
    h: shown.h + (target.h - shown.h) * share,
  }
  const scale = uiScale()
  if (Math.abs(target.x - shown.x) < settledPx && Math.abs(target.y - shown.y) < settledPx) {
    shown = { ...target }
    paint(shown, scale)
    return
  }
  paint(shown, scale)
  frame = requestAnimationFrame(step)
}

function update(): void {
  queued = false
  const el = editable(document.activeElement)
  const on = get(prefs).smoothCaret && get(prefs).design !== 'classic'
  if (!on || !el) {
    release()
    return
  }
  if (owner !== el) {
    release()
    owner = el
    el.setAttribute('data-smooth-caret', '')
  }
  const scale = uiScale()
  const spot = isField(el) ? fieldSpot(el, scale) : richSpot(el)
  if (!spot || !visible(el, spot) || !caret) {
    hide()
    return
  }
  // a look that finds the caret where it already was changes nothing, so the
  // blink is left running.
  const moved = !target || Math.abs(target.x - spot.x) > settledPx || Math.abs(target.y - spot.y) > settledPx
  if (!moved && caret.classList.contains('shown')) {
    return
  }
  target = spot
  const reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches || get(prefs).reduceMotion === true
  if (!shown || reduce) {
    // nowhere to glide from, or no gliding wanted.
    shown = { ...spot }
    paint(shown, scale)
  } else if (!frame) {
    // the first stretch is covered at once, as if a frame had already passed,
    // so the caret leaves with the keystroke instead of a frame after it.
    last = performance.now() - 16
    step(performance.now())
  }
  caret.classList.add('shown')
  // it holds steady while it moves and blinks once typing rests.
  caret.classList.remove('blink')
  clearTimeout(idle)
  idle = setTimeout(() => caret?.classList.add('blink'), idleMs)
}

function queue(): void {
  if (!queued) {
    queued = true
    requestAnimationFrame(update)
  }
}

// typing and caret moves are measured in the same turn they happen, before
// the frame is painted, so the caret is never a frame behind the text.
function now(): void {
  queueMicrotask(update)
}

/** Starts the gliding caret for this window. Call once, at startup. */
export function initSmoothCaret(): void {
  if (caret) {
    return
  }
  caret = document.createElement('div')
  caret.className = 'smooth-caret'
  caret.setAttribute('aria-hidden', 'true')
  document.body.appendChild(caret)
  for (const type of ['selectionchange', 'input', 'compositionend']) {
    document.addEventListener(type, now, true)
  }
  for (const type of ['focusin', 'focusout', 'keyup', 'pointerup']) {
    document.addEventListener(type, queue, true)
  }
  document.addEventListener('scroll', queue, true)
  window.addEventListener('resize', queue)
  window.addEventListener('blur', queue)
  document.addEventListener('keydown', queue, true)
  // nothing announces a screen opening over the focused field, so while a
  // field holds the caret its place is looked at again a few times a second.
  setInterval(() => {
    if (owner) {
      queue()
    }
  }, recheckMs)
  prefs.subscribe(queue)
}
