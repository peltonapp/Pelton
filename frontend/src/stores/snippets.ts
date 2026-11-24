// snippets.ts holds the user's snippets for the composer and the settings
// page. Changes go to the backend first and the list is replaced with what it
// stored, so a refused trigger never shows as saved.

import { get, writable } from 'svelte/store'
import type { Snippet } from '../lib/types'
import { deleteSnippet, listSnippets, reorderSnippets, saveSnippet } from '../lib/api'

/** Every snippet in the user's order. */
export const snippets = writable<Snippet[]>([])

/** loadSnippets reads the snippets again; a failure keeps the last list. */
export async function loadSnippets(): Promise<void> {
  try {
    snippets.set(await listSnippets())
  } catch {
    // the list on screen is still the last good one.
  }
}

/** storeSnippet saves a new or edited snippet and returns it as stored. */
export async function storeSnippet(snippet: Snippet): Promise<Snippet> {
  const saved = await saveSnippet(snippet)
  snippets.update((list) =>
    list.some((s) => s.id === saved.id) ? list.map((s) => (s.id === saved.id ? saved : s)) : [...list, saved],
  )
  return saved
}

/** removeSnippet deletes a snippet. */
export async function removeSnippet(id: number): Promise<void> {
  await deleteSnippet(id)
  snippets.update((list) => list.filter((s) => s.id !== id))
}

/**
 * moveSnippets puts the snippets in the given order at once and stores it,
 * going back to the old order when the backend refuses.
 */
export async function moveSnippets(ids: number[]): Promise<void> {
  const before = get(snippets)
  const byId = new Map(before.map((s) => [s.id, s]))
  snippets.set(ids.map((id) => byId.get(id)).filter((s): s is Snippet => s !== undefined))
  try {
    await reorderSnippets(ids)
  } catch (err) {
    snippets.set(before)
    throw err
  }
}
