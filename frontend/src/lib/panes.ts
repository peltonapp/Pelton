// panes.ts moves keyboard focus between the three panes: the sidebar, the
// message list and the reading pane (#495).

export type Pane = 'sidebar' | 'list' | 'reader'

const order: Pane[] = ['sidebar', 'list', 'reader']

// paneRoots are the elements a pane's focus can sit anywhere inside.
const paneRoots: Record<Pane, string> = {
  sidebar: '.sidebar',
  list: '.list-col',
  reader: '.detail',
}

// paneTarget is what takes focus in a pane: the selected sidebar row, the
// message list itself (which arrows and j/k move through), or the reading
// pane's scroller (which arrows and page keys scroll).
function paneTarget(pane: Pane): HTMLElement | null {
  switch (pane) {
    case 'sidebar':
      return (
        document.querySelector<HTMLElement>('.sidebar .row.active button') ??
        document.querySelector<HTMLElement>('.sidebar button')
      )
    case 'list':
      return document.querySelector<HTMLElement>('.list-col [role="listbox"]')
    case 'reader':
      return document.querySelector<HTMLElement>('.detail .scroll')
  }
}

// currentPane names the pane holding focus, or null outside all three.
export function currentPane(el: Element | null): Pane | null {
  if (!el) {
    return null
  }
  return order.find((pane) => el.closest(paneRoots[pane])) ?? null
}

// focusPane moves focus into a pane, reporting whether there was anything to
// focus there (the reading pane is empty with no message open).
export function focusPane(pane: Pane): boolean {
  const target = paneTarget(pane)
  if (!target) {
    return false
  }
  target.focus()
  return true
}

// cyclePane moves focus to the next (1) or previous (-1) pane, wrapping around
// and skipping a pane with nothing to focus. From outside the panes it starts
// at the message list.
export function cyclePane(dir: 1 | -1): void {
  const from = currentPane(document.activeElement)
  if (from === null) {
    focusPane('list')
    return
  }
  let index = order.indexOf(from)
  for (let step = 0; step < order.length - 1; step++) {
    index = (index + dir + order.length) % order.length
    if (focusPane(order[index])) {
      return
    }
  }
}
