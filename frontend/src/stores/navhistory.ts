// navhistory.ts is the window's back and forward history: the places the user
// has been, across spaces, for this session only. it follows the space on
// screen and whatever the space's location provider watches, and records a
// visit when the place changes. going back or forward shows a stored place
// again through its provider; a place that no longer exists is dropped and the
// next one in that direction is tried. it imports no space and no space store:
// spaces register their providers in lib/locations.ts.

import { derived, get, writable, type Readable } from 'svelte/store'
import { locationKey, locationProvider, locationProviders, onProviderRegistered, type Location } from '../lib/locations'
import { openSpace, space } from './space'
import type { Space } from '../lib/types'

/** One place in the history. */
export interface Visit {
  loc: Location
  /** Equal for two visits of the same place. */
  key: string
  /** What the place shows, or empty for the space alone. */
  label: string
}

/** The visits in order, oldest first, and the one on screen. */
export interface NavState {
  visits: Visit[]
  index: number
}

/** A visit's name for menus: "Space: what was open", or the space alone. */
export function visitLabel(visit: Visit, t: (key: string) => string): string {
  const name = t(`space.${visit.loc.space}`)
  return visit.label ? `${name}: ${visit.label}` : name
}

/** How many visits the history keeps; older ones fall off the front. */
export const navCap = 50

/** A history with nothing in it. */
export const emptyNav: NavState = { visits: [], index: -1 }

/**
 * Records a visit. The same place as the current one only refreshes its
 * label. A move that just opens or closes a detail in place, as refines says,
 * replaces the current visit. Anything else drops the visits ahead of the
 * current one and adds the new one, keeping at most cap.
 */
export function pushVisit(
  state: NavState,
  visit: Visit,
  refines?: (from: Location, to: Location) => boolean,
  cap = navCap,
): NavState {
  const cur = state.visits[state.index]
  if (cur && cur.key === visit.key) {
    if (!visit.label || visit.label === cur.label) {
      return state
    }
    return replaceCurrent(state, { ...cur, label: visit.label })
  }
  if (cur && cur.loc.space === visit.loc.space && refines?.(cur.loc, visit.loc)) {
    return replaceCurrent(state, visit)
  }
  const visits = [...state.visits.slice(0, state.index + 1), visit]
  const trimmed = visits.length > cap ? visits.slice(visits.length - cap) : visits
  return { visits: trimmed, index: trimmed.length - 1 }
}

/** Puts a visit in place of the current one, keeping the ones around it. */
export function replaceCurrent(state: NavState, visit: Visit): NavState {
  if (state.index < 0) {
    return { visits: [visit], index: 0 }
  }
  const visits = [...state.visits]
  visits[state.index] = visit
  return { visits, index: state.index }
}

/** Takes a visit out, keeping the current one current. */
export function dropVisit(state: NavState, at: number): NavState {
  if (at < 0 || at >= state.visits.length) {
    return state
  }
  const visits = state.visits.filter((_, i) => i !== at)
  const index = at < state.index ? state.index - 1 : Math.min(state.index, visits.length - 1)
  return { visits, index }
}

const state = writable<NavState>(emptyNav)

/** The history of this window. */
export const navHistory: Readable<NavState> = { subscribe: state.subscribe }

/** Whether there is a place to go back to. */
export const canGoBack = derived(state, ($s) => $s.index > 0)

/** Whether there is a place to go forward to. */
export const canGoForward = derived(state, ($s) => $s.index >= 0 && $s.index < $s.visits.length - 1)

/** The places behind the current one, nearest first, with their index. */
export const backVisits = derived(state, ($s) => $s.visits.slice(0, Math.max(0, $s.index)).map((visit, i) => ({ visit, index: i })).reverse())

/** The places ahead of the current one, nearest first, with their index. */
export const forwardVisits = derived(state, ($s) => $s.visits.slice($s.index + 1).map((visit, i) => ({ visit, index: $s.index + 1 + i })))

// --- following the app -------------------------------------------------------

let restoring = 0
let busy = false
let scheduled = false
let running = false
const watched = new Set<Readable<unknown>>()
let stops: (() => void)[] = []

function describe(key: Space): Location {
  const provider = locationProvider(key)
  if (!provider) {
    return { space: key }
  }
  try {
    const loc = provider.describe()
    return loc.space === key ? loc : { space: key }
  } catch {
    return { space: key }
  }
}

function labelOf(loc: Location): string {
  try {
    return locationProvider(loc.space)?.label(loc) ?? ''
  } catch {
    return ''
  }
}

function visitOf(loc: Location): Visit {
  return { loc, key: locationKey(loc), label: labelOf(loc) }
}

function refinesFor(key: Space): ((a: Location, b: Location) => boolean) | undefined {
  const provider = locationProvider(key)
  if (!provider?.refines) {
    return undefined
  }
  return (a, b) => {
    try {
      return provider.refines?.(a, b) ?? false
    } catch {
      return false
    }
  }
}

function commit(): void {
  if (!running || restoring > 0) {
    return
  }
  const key = get(space)
  state.update((s) => pushVisit(s, visitOf(describe(key)), refinesFor(key)))
}

// several stores change in one go when the user moves (the list and the open
// message, say), so a change is recorded once they have all settled.
function schedule(): void {
  if (scheduled) {
    return
  }
  scheduled = true
  queueMicrotask(() => {
    scheduled = false
    commit()
  })
}

function follow(stores: Readable<unknown>[]): void {
  for (const store of stores) {
    if (!watched.has(store)) {
      watched.add(store)
      stops.push(store.subscribe(schedule))
    }
  }
}

/**
 * Starts recording, beginning with the place on screen now. Returns the
 * function that stops it and forgets the history.
 */
export function startNavHistory(): () => void {
  running = true
  follow([space])
  for (const provider of locationProviders()) {
    follow(provider.watch)
  }
  stops.push(onProviderRegistered((provider) => follow(provider.watch)))
  schedule()
  return () => {
    running = false
    for (const stop of stops) {
      stop()
    }
    stops = []
    watched.clear()
    state.set(emptyNav)
  }
}

/** Forgets every visit but the place on screen now. */
export function clearNavHistory(): void {
  state.set(emptyNav)
  schedule()
}

function frame(): Promise<void> {
  return new Promise((resolve) => {
    if (typeof requestAnimationFrame === 'function') {
      requestAnimationFrame(() => resolve())
    } else {
      setTimeout(resolve, 16)
    }
  })
}

async function restore(loc: Location): Promise<boolean> {
  const provider = locationProvider(loc.space)
  try {
    if (!provider) {
      openSpace(loc.space)
      return true
    }
    return await provider.restore(loc)
  } catch {
    return false
  }
}

// travel shows the visit at `at`, or the nearest one beyond it in the
// direction of travel that still exists. Visits that fail are dropped.
async function travel(at: number, dir: 1 | -1): Promise<boolean> {
  if (busy || !running) {
    return false
  }
  busy = true
  restoring += 1
  try {
    let i = at
    for (;;) {
      const s = get(state)
      if (i < 0 || i >= s.visits.length || i === s.index) {
        return false
      }
      const target = s.visits[i]
      if (await restore(target.loc)) {
        state.update((cur) => ({ ...cur, index: Math.max(0, cur.visits.indexOf(target)) }))
        return true
      }
      state.update((cur) => dropVisit(cur, cur.visits.indexOf(target)))
      if (dir < 0) {
        i -= 1
      }
    }
  } finally {
    // the space settles over a frame or two, then the visit takes on what it
    // actually shows, in place, so the visits ahead are kept.
    await frame()
    await frame()
    restoring -= 1
    busy = false
    const key = get(space)
    const s = get(state)
    const cur = s.visits[s.index]
    if (running && cur && cur.loc.space === key) {
      const now = visitOf(describe(key))
      if (now.key !== cur.key) {
        state.update((x) => replaceCurrent(x, now))
      }
    }
  }
}

/** Shows the previous place. False when there was none that still exists. */
export function goBack(): Promise<boolean> {
  return travel(get(state).index - 1, -1)
}

/** Shows the next place. False when there was none that still exists. */
export function goForward(): Promise<boolean> {
  return travel(get(state).index + 1, 1)
}

/** Shows the place at an index of the history, from the menu of recent places. */
export function goToVisit(index: number): Promise<boolean> {
  const cur = get(state).index
  return travel(index, index < cur ? -1 : 1)
}
