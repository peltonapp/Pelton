// scriptmark.ts is the text side of the script mark: the lightning the app
// draws beside every button and entry that runs a script. The mark itself is
// ScriptMark.svelte, drawn by the app and never from script data; here a
// script's name or label loses any character that looks like it, and the
// accessible name of a script entry says it is a script.

const markGlyphs = /[⚡☇↯ϟ\u{1F5F2}]️?/gu

/** A script's name or label as the app shows it, with any lookalike of the script mark taken out. */
export function scriptText(text: string): string {
  return text.replace(markGlyphs, '').replace(/\s{2,}/g, ' ').trim()
}

/**
 * The tooltip and accessible name of an entry that runs a script: its label
 * and "Script: name", or only the latter when the label is the name.
 */
export function scriptEntryName(label: string, name: string, say: (key: string) => string): string {
  const shownName = scriptText(name)
  const shownLabel = scriptText(label)
  const script = say('scripts.mark.name').replace('{name}', shownName)
  return shownLabel === '' || shownLabel === shownName ? script : `${shownLabel}, ${script}`
}
