// palettesources.ts lets the spaces beside mail put their own entries into the
// command palette. lib/commands.ts builds the mail entries from a context and
// stays free of stores; a space registers a source here instead, a function
// that returns its entries when the palette opens.

import type { PaletteCommand } from './commands'

/** Builds a space's palette entries; `t` resolves a localization key. */
export type PaletteSource = (t: (key: string) => string) => PaletteCommand[]

const sources = new Map<string, PaletteSource>()

/** Adds a source under a name; registering the name again replaces it. */
export function registerPaletteSource(name: string, source: PaletteSource): void {
  sources.set(name, source)
}

/** The entries of every registered source, in registration order. */
export function extraPaletteCommands(t: (key: string) => string): PaletteCommand[] {
  const out: PaletteCommand[] = []
  for (const source of sources.values()) {
    out.push(...source(t))
  }
  return out
}
