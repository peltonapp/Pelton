// search.ts holds the top bar search of the new design: what is being typed
// (the draft), the search the list is showing (active), the recent searches,
// and the words the result rows and the reading pane highlight. The message
// list runs the active search; this store only says what it is.

import { derived, get, writable } from 'svelte/store'
import { getSetting, setSetting, SettingKeys } from '../lib/api'
import { addChip, removeChip, searchTerms, toggleChip, type SearchChip } from '../lib/searchquery'

/** A search: free text plus committed chips. */
export interface SearchState {
  text: string
  chips: SearchChip[]
}

/** How many recent searches are kept. */
export const recentLimit = 10

const blank = (): SearchState => ({ text: '', chips: [] })

/** What the top bar field holds right now. */
export const searchDraft = writable<SearchState>(blank())

/** The search the list is showing, or null when it shows the open folder. */
export const activeSearch = writable<SearchState | null>(null)

/** The last submitted searches, newest first. */
export const recentSearches = writable<SearchState[]>([])

/** The message opened from a search result, so the reader marks the matches. */
export const searchOpenedId = writable<number | null>(null)

/** The words the active search matched on. */
export const highlightTerms = derived(activeSearch, ($active) => ($active ? searchTerms($active.text, $active.chips) : []))

/** Reports whether a search constrains nothing. */
export function isBlank(s: SearchState): boolean {
  return s.text.trim() === '' && s.chips.length === 0
}

function same(a: SearchState, b: SearchState): boolean {
  return JSON.stringify(normalize(a)) === JSON.stringify(normalize(b))
}

function normalize(s: SearchState): SearchState {
  return { text: s.text.trim(), chips: s.chips.map(({ field, value, id, label }) => ({ field, value, id, label })) }
}

/**
 * Runs the draft: the list switches to its results and the search goes to the
 * front of the recent list. A blank draft ends the search instead.
 */
export function submitSearch(): void {
  const draft = normalize(get(searchDraft))
  if (isBlank(draft)) {
    clearSearch()
    return
  }
  activeSearch.set(draft)
  recordRecent(draft)
}

/** Replaces the draft, for a recent search picked from the list. */
export function setDraft(state: SearchState): void {
  searchDraft.set(normalize(state))
}

/** Replaces the draft text. */
export function setDraftText(text: string): void {
  searchDraft.update((s) => ({ ...s, text }))
}

// changeChips edits the draft's chips; while results are showing the change
// refines them at once.
function changeChips(edit: (chips: SearchChip[]) => SearchChip[]): void {
  searchDraft.update((s) => ({ ...s, chips: edit(s.chips) }))
  if (get(activeSearch)) {
    const draft = normalize(get(searchDraft))
    if (isBlank(draft)) {
      clearSearch()
      return
    }
    activeSearch.set({ ...draft, text: get(activeSearch)?.text ?? draft.text })
  }
}

/** Commits a chip, replacing one of the same kind. */
export function commitChip(chip: SearchChip): void {
  changeChips((chips) => addChip(chips, chip))
}

/** Removes a chip. */
export function dropChip(chip: SearchChip): void {
  changeChips((chips) => removeChip(chips, chip))
}

/** Adds the chip when absent and removes it when present. */
export function flipChip(chip: SearchChip): void {
  changeChips((chips) => toggleChip(chips, chip))
}

/** Replaces every chip at once. */
export function setChips(chips: SearchChip[]): void {
  changeChips(() => chips)
}

/** Empties the field and returns the list to the open folder. */
export function clearSearch(): void {
  searchDraft.set(blank())
  activeSearch.set(null)
  searchOpenedId.set(null)
}

/** Loads the recent searches from settings. A bad value reads as none. */
export async function loadRecentSearches(): Promise<void> {
  try {
    const stored = await getSetting(SettingKeys.recentSearches)
    if (!stored.found) {
      return
    }
    const parsed: unknown = JSON.parse(stored.value)
    if (!Array.isArray(parsed)) {
      return
    }
    const valid = parsed.filter(
      (s): s is SearchState => typeof s?.text === 'string' && Array.isArray(s?.chips),
    )
    recentSearches.set(valid.slice(0, recentLimit))
  } catch {
    recentSearches.set([])
  }
}

function saveRecent(list: SearchState[]): void {
  void setSetting(SettingKeys.recentSearches, JSON.stringify(list)).catch(() => undefined)
}

/** Puts a search at the front of the recent list, without duplicates. */
export function recordRecent(state: SearchState): void {
  const entry = normalize(state)
  const list = [entry, ...get(recentSearches).filter((s) => !same(s, entry))].slice(0, recentLimit)
  recentSearches.set(list)
  saveRecent(list)
}

/** Forgets every recent search. */
export function clearRecentSearches(): void {
  recentSearches.set([])
  saveRecent([])
}
