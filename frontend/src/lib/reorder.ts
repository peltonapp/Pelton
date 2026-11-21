// reorder.ts is the sidebar's drag-to-reorder action.
//
// Two things it deliberately does not do. It does not lift a floating copy of
// the row under the cursor: the row stays put and dims, so nothing has to be
// positioned against the pointer and nothing can drift away from it. And it
// does not draw the drop indicator at computed coordinates: it inserts a real
// element into the list, so the rows part to show the gap and the indicator is
// always exactly between two of them by construction, with no rect arithmetic
// to get wrong.
//
// Reordering is always within one container, which is what makes "siblings
// only" true by construction: there is no cross-container drop to refuse.
//
// It works along either axis. Vertical is the default (the sidebar); the
// reading-pane tab strip passes axis: 'x'. Only the measurements differ, so
// both share the same drag, placeholder and auto-scroll behaviour.
//
// Usage: put the action on the container, mark each direct child with
// data-reorder-id, and mark the grab handle inside a child with
// data-reorder-handle. A drag can only start from a handle, so ordinary clicks
// on the row still select.

import type { ActionReturn } from 'svelte/action'

/** Detail of the `reorder` event: the child ids in their new order. */
export interface ReorderDetail {
  ids: string[]
}

/** Options for the action. */
export interface ReorderOptions {
  /** Which way the items are laid out. Defaults to vertical. */
  axis?: 'x' | 'y'
  /**
   * Lets an item be torn off: released far enough across the axis (off a tab
   * strip, say), it fires `reordertearoff` instead of reordering.
   */
  tearOff?: boolean
}

/** The detail of a `reordertearoff` event. */
export interface TearOffDetail {
  /** The data-reorder-id of the item that was torn off. */
  id: string
}

/** The events the action adds to the element it is used on. */
interface ReorderAttributes {
  'on:reorder'?: (event: CustomEvent<ReorderDetail>) => void
  'on:reorderstart'?: (event: CustomEvent<void>) => void
  'on:reorderend'?: (event: CustomEvent<void>) => void
  'on:reordertearoff'?: (event: CustomEvent<TearOffDetail>) => void
}

// how far across the axis an item has to travel to be torn off rather than
// reordered.
const tearOffDistance = 56

// pointer travel before a press counts as a drag rather than a click.
const dragThreshold = 4
// how close to the edge of the scroll container before it scrolls, and by how
// much per frame.
const autoScrollMargin = 28
const autoScrollSpeed = 8

/** Attribute marking a direct child of the container as a reorderable item. */
export const reorderIdAttr = 'data-reorder-id'
/** Attribute marking the grab handle inside an item. */
export const reorderHandleAttr = 'data-reorder-handle'

// nearestScroller finds the element that actually scrolls along the drag axis,
// so a drag near the edge of a long folder list, or a tab strip wider than the
// pane, can pull it along. The container itself counts: a tab strip scrolls
// itself rather than delegating to an ancestor.
function nearestScroller(el: HTMLElement, horizontal: boolean): HTMLElement | null {
  for (let node: HTMLElement | null = el; node; node = node.parentElement) {
    const style = getComputedStyle(node)
    const overflow = horizontal ? style.overflowX : style.overflowY
    const scrolls = horizontal
      ? node.scrollWidth > node.clientWidth
      : node.scrollHeight > node.clientHeight
    if ((overflow === 'auto' || overflow === 'scroll') && scrolls) {
      return node
    }
  }
  return null
}

// newPlaceholder builds the gap that opens between items at the drop position.
// It is a plain list element, so it cannot land on top of an item.
function newPlaceholder(horizontal: boolean): HTMLDivElement {
  const el = document.createElement('div')
  if (horizontal) {
    el.style.width = '2px'
    el.style.margin = 'var(--space-1) 2px'
    el.style.alignSelf = 'stretch'
  } else {
    el.style.height = '2px'
    el.style.margin = '2px var(--space-2)'
  }
  el.style.flex = '0 0 auto'
  el.style.borderRadius = '1px'
  el.style.background = 'var(--accent)'
  el.style.pointerEvents = 'none'
  return el
}

/**
 * reorder makes a container's children draggable into a new order. It emits
 * `reorderstart` when a drag begins, `reorderend` when it ends either way, and
 * `reorder` with a {@link ReorderDetail} only when the order actually changed.
 */
export function reorder(
  node: HTMLElement,
  options: ReorderOptions = {},
): ActionReturn<ReorderOptions | undefined, ReorderAttributes> {
  const horizontal = options.axis === 'x'
  // the pointer coordinate along the drag axis, and the item edges to compare
  // it against. Everything else is the same either way.
  const along = (event: MouseEvent): number => (horizontal ? event.clientX : event.clientY)
  const across = (event: MouseEvent): number => (horizontal ? event.clientY : event.clientX)
  const tearOff = options.tearOff ?? false
  const midpoint = (rect: DOMRect): number => (horizontal ? rect.left + rect.width / 2 : rect.top + rect.height / 2)

  let dragged: HTMLElement | null = null
  let start = 0
  let active = false
  let placeholder: HTMLDivElement | null = null
  let dropIndex = -1
  let scroller: HTMLElement | null = null
  let scrollFrame = 0
  let pointer = 0
  // where the press started across the axis, and whether the pointer is now
  // far enough off it that letting go tears the item off.
  let crossStart = 0
  let tornOff = false

  // only the real items, so the placeholder never counts as one.
  function items(): HTMLElement[] {
    return Array.from(node.children).filter(
      (el): el is HTMLElement => el instanceof HTMLElement && el.hasAttribute(reorderIdAttr),
    )
  }

  function onMouseDown(event: MouseEvent): void {
    if (event.button !== 0) {
      return
    }
    const target = event.target as HTMLElement | null
    const handle = target?.closest<HTMLElement>(`[${reorderHandleAttr}]`)
    if (!handle) {
      return
    }
    const item = handle.closest<HTMLElement>(`[${reorderIdAttr}]`)
    // a nested container's items are also inside this one, and the event
    // bubbles through both. only the container the item actually belongs to
    // handles it.
    if (!item || item.parentElement !== node) {
      return
    }
    event.preventDefault()
    dragged = item
    start = along(event)
    pointer = along(event)
    crossStart = across(event)
    tornOff = false
    scroller = nearestScroller(node, horizontal)
    window.addEventListener('mousemove', onMouseMove)
    window.addEventListener('mouseup', onMouseUp)
    window.addEventListener('keydown', onKeyDown)
  }

  function begin(): void {
    active = true
    if (dragged) {
      dragged.style.opacity = '0.4'
    }
    placeholder = newPlaceholder(horizontal)
    node.dispatchEvent(new CustomEvent('reorderstart'))
  }

  // dropIndexFor counts how many items sit before the pointer, which is the
  // index the dragged item would take.
  function dropIndexFor(at: number, list: HTMLElement[]): number {
    let index = 0
    for (const el of list) {
      if (at < midpoint(el.getBoundingClientRect())) {
        break
      }
      index++
    }
    return index
  }

  // update re-measures and moves the gap. The placeholder comes out first so the
  // rows are measured where they sit without it, which keeps the index stable
  // instead of oscillating around the gap it just opened. Removing and
  // reinserting in one go never paints in between, so there is no flicker.
  function update(): void {
    if (!placeholder || !dragged) {
      return
    }
    placeholder.remove()
    const list = items()
    if (list.length === 0) {
      return
    }
    const index = dropIndexFor(pointer, list)
    dropIndex = index
    const from = list.indexOf(dragged)
    // dropping back into its own slot changes nothing, so show no gap at all
    // rather than a gap the row would just fall straight back into.
    if (from >= 0 && (index === from || index === from + 1)) {
      return
    }
    node.insertBefore(placeholder, list[index] ?? null)
  }

  function onMouseMove(event: MouseEvent): void {
    pointer = along(event)
    const off = tearOff && Math.abs(across(event) - crossStart) > tearOffDistance
    if (!active && !off && Math.abs(pointer - start) < dragThreshold) {
      return
    }
    if (!active) {
      begin()
    }
    tornOff = off
    if (tornOff) {
      // off the strip: no gap to drop into, and the item fades further to say
      // it is leaving rather than moving.
      placeholder?.remove()
      if (dragged) {
        dragged.style.opacity = '0.15'
      }
      return
    }
    if (dragged) {
      dragged.style.opacity = '0.4'
    }
    update()
    startAutoScroll()
  }

  function startAutoScroll(): void {
    if (scrollFrame || !scroller) {
      return
    }
    const step = (): void => {
      scrollFrame = 0
      if (!active || !scroller) {
        return
      }
      const rect = scroller.getBoundingClientRect()
      const near = horizontal ? rect.left : rect.top
      const far = horizontal ? rect.right : rect.bottom
      let delta = 0
      if (pointer < near + autoScrollMargin) {
        delta = -autoScrollSpeed
      } else if (pointer > far - autoScrollMargin) {
        delta = autoScrollSpeed
      }
      if (delta !== 0) {
        if (horizontal) {
          scroller.scrollLeft += delta
        } else {
          scroller.scrollTop += delta
        }
        update()
        scrollFrame = requestAnimationFrame(step)
      }
    }
    scrollFrame = requestAnimationFrame(step)
  }

  function onKeyDown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      finish(false)
    }
  }

  function onMouseUp(): void {
    finish(true)
  }

  function finish(commit: boolean): void {
    window.removeEventListener('mousemove', onMouseMove)
    window.removeEventListener('mouseup', onMouseUp)
    window.removeEventListener('keydown', onKeyDown)
    if (scrollFrame) {
      cancelAnimationFrame(scrollFrame)
      scrollFrame = 0
    }

    const wasActive = active
    const item = dragged
    const index = dropIndex
    active = false
    dragged = null
    dropIndex = -1
    scroller = null

    if (item) {
      item.style.opacity = ''
    }
    if (placeholder) {
      placeholder.remove()
      placeholder = null
    }
    if (!wasActive) {
      return
    }
    node.dispatchEvent(new CustomEvent('reorderend'))
    const torn = tornOff
    tornOff = false
    if (commit && torn && item) {
      const id = item.getAttribute(reorderIdAttr) ?? ''
      node.dispatchEvent(new CustomEvent<TearOffDetail>('reordertearoff', { detail: { id } }))
      return
    }
    if (!commit || !item || index < 0) {
      return
    }

    const list = items()
    const from = list.indexOf(item)
    // an insertion point below the row's own slot shifts up by one once the row
    // is lifted out, so dropping just below where it already is is a no-op.
    const to = index > from ? index - 1 : index
    if (from < 0 || to === from) {
      return
    }
    const ids = list.map((el) => el.getAttribute(reorderIdAttr) ?? '')
    const [moved] = ids.splice(from, 1)
    ids.splice(to, 0, moved)
    node.dispatchEvent(new CustomEvent<ReorderDetail>('reorder', { detail: { ids } }))
  }

  node.addEventListener('mousedown', onMouseDown)

  return {
    destroy(): void {
      node.removeEventListener('mousedown', onMouseDown)
      finish(false)
    },
  }
}
