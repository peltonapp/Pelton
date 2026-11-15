// tip.ts is the tooltip of the new design: a small label, with an optional
// shortcut, that appears beside an element after a short hover or on keyboard
// focus. it is an action so any element can carry one without wrapper markup.
// the bubble is styled by .tip in style.css, since it is appended to the body.

/** What a tooltip shows and where. */
export interface TipOptions {
  /** The label. An empty label shows nothing. */
  text: string
  /** Shortcut hint shown after the label, already formatted for display. */
  shortcut?: string
  /** Which side of the element the bubble opens on. */
  side?: 'top' | 'bottom' | 'left' | 'right'
}

const showDelay = 380
const gap = 8

let bubble: HTMLDivElement | null = null
let timer: ReturnType<typeof setTimeout> | undefined

function hide(): void {
  clearTimeout(timer)
  if (bubble) {
    bubble.remove()
    bubble = null
  }
}

function show(node: Element, options: TipOptions): void {
  hide()
  if (!options.text) {
    return
  }
  const el = document.createElement('div')
  el.className = 'tip'
  el.setAttribute('role', 'tooltip')
  const label = document.createElement('span')
  label.textContent = options.text
  el.appendChild(label)
  if (options.shortcut) {
    const keys = document.createElement('kbd')
    keys.textContent = options.shortcut
    el.appendChild(keys)
  }
  document.body.appendChild(el)
  bubble = el

  // the root carries a css zoom for the interface scale; client rects are in
  // zoomed pixels while fixed offsets are not, so divide it back out.
  const scale = Number(getComputedStyle(document.documentElement).getPropertyValue('--ui-scale')) || 1
  const rect = node.getBoundingClientRect()
  const own = el.getBoundingClientRect()
  const side = options.side ?? 'bottom'
  let left = rect.left + rect.width / 2 - own.width / 2
  let top = rect.bottom + gap
  if (side === 'top') {
    top = rect.top - own.height - gap
  } else if (side === 'right') {
    left = rect.right + gap
    top = rect.top + rect.height / 2 - own.height / 2
  } else if (side === 'left') {
    left = rect.left - own.width - gap
    top = rect.top + rect.height / 2 - own.height / 2
  }
  left = Math.max(4, Math.min(left, window.innerWidth - own.width - 4))
  top = Math.max(4, Math.min(top, window.innerHeight - own.height - 4))
  el.style.left = `${left / scale}px`
  el.style.top = `${top / scale}px`
  el.dataset.side = side
  el.classList.add('shown')
}

/** Shows a tooltip beside the element on hover and keyboard focus; svg shapes take one too. */
export function tip(node: HTMLElement | SVGElement, options: TipOptions): { update(next: TipOptions): void; destroy(): void } {
  let current = options

  function onEnter(): void {
    clearTimeout(timer)
    timer = setTimeout(() => show(node, current), showDelay)
  }

  function onFocus(): void {
    if (node.matches(':focus-visible')) {
      show(node, current)
    }
  }

  node.addEventListener('pointerenter', onEnter)
  node.addEventListener('pointerleave', hide)
  node.addEventListener('pointerdown', hide)
  node.addEventListener('focus', onFocus)
  node.addEventListener('blur', hide)

  return {
    update(next: TipOptions): void {
      current = next
    },
    destroy(): void {
      hide()
      node.removeEventListener('pointerenter', onEnter)
      node.removeEventListener('pointerleave', hide)
      node.removeEventListener('pointerdown', hide)
      node.removeEventListener('focus', onFocus)
      node.removeEventListener('blur', hide)
    },
  }
}
