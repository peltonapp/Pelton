// spacesearch.ts lets a space other than mail put its own search in the top
// bar: the calendar searches events, contacts search people, feeds search
// entries. the space's screen registers a provider while it is on screen.

import { writable } from 'svelte/store'
import type { Space } from './types'

/** One hit in the top bar's dropdown. */
export interface SpaceSearchResult {
  id: string
  title: string
  /** Second line, under the title. */
  detail?: string
  /** Short text on the far side, such as a date. */
  aside?: string
  /** A css color for the dot before the title. */
  color?: string
}

export interface SpaceSearchProvider {
  /** i18n key of the field's placeholder. */
  placeholderKey: string
  /** i18n key of the line shown when nothing matches. */
  emptyKey: string
  /** The hits for a query that is not empty, best first. */
  search: (query: string) => Promise<SpaceSearchResult[]> | SpaceSearchResult[]
  /** Shows a hit in the space. */
  open: (result: SpaceSearchResult) => void
}

/** The providers of the spaces that have one right now. */
export const spaceSearchProviders = writable<Partial<Record<Space, SpaceSearchProvider>>>({})

/** Gives a space its top bar search; returns the function that takes it away. */
export function registerSpaceSearch(key: Space, provider: SpaceSearchProvider): () => void {
  spaceSearchProviders.update((all) => ({ ...all, [key]: provider }))
  return () => {
    spaceSearchProviders.update((all) => {
      if (all[key] !== provider) {
        return all
      }
      const rest = { ...all }
      delete rest[key]
      return rest
    })
  }
}

/** Puts the cursor in the space's top bar search. False when there is none. */
export function focusSpaceSearch(): boolean {
  const input = document.querySelector<HTMLInputElement>('input[data-space-search]')
  if (!input) {
    return false
  }
  input.focus()
  input.select()
  return true
}

/** True when every word of the query is in one of the texts, ignoring case. */
export function matchesQuery(query: string, texts: readonly string[]): boolean {
  const hay = texts.join('\n').toLocaleLowerCase()
  return query
    .toLocaleLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((word) => hay.includes(word))
}
