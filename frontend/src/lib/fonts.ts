// fonts.ts is the curated list of reader fonts: what the mail body iframe
// falls back to when a message does not declare its own fonts. keys are what
// the body_font setting stores; stacks are plain css font-family values (the
// iframe cannot resolve the app's css variables, so the default resolves to
// the ui font at build time in MailBody). shared with compose so written and
// read mail can use the same face (#64).

export interface BodyFont {
  key: string
  // labelKey resolves through i18n for the two generic entries; the named
  // families are shown as their own name (fonts are proper nouns).
  labelKey?: string
  label?: string
  // stack is null for the default entry, which uses the app's ui font.
  stack: string | null
}

export const bodyFonts: BodyFont[] = [
  { key: 'system', labelKey: 'settingsPanel.bodyFont.system', stack: 'system-ui, -apple-system, "Segoe UI", Roboto, sans-serif' },
  { key: 'default', labelKey: 'settingsPanel.bodyFont.default', stack: null },
  { key: 'serif', labelKey: 'settingsPanel.bodyFont.serif', stack: 'Georgia, "Times New Roman", Times, serif' },
  { key: 'sans', labelKey: 'settingsPanel.bodyFont.sans', stack: 'Helvetica, Arial, sans-serif' },
  { key: 'mono', labelKey: 'settingsPanel.bodyFont.mono', stack: 'ui-monospace, "SF Mono", Consolas, "Liberation Mono", monospace' },
  { key: 'arial', label: 'Arial', stack: 'Arial, Helvetica, sans-serif' },
  { key: 'verdana', label: 'Verdana', stack: 'Verdana, Geneva, sans-serif' },
  { key: 'georgia', label: 'Georgia', stack: 'Georgia, serif' },
  { key: 'times', label: 'Times New Roman', stack: '"Times New Roman", Times, serif' },
  { key: 'courier', label: 'Courier New', stack: '"Courier New", Courier, monospace' },
]

// uiFonts are the interface font choices (#58): they flow into the --font-ui
// token, so everything outside rendered mail follows. default is the bundled
// Familjen Grotesk from tokens.css.
export const uiFonts: BodyFont[] = [
  { key: 'default', labelKey: 'settingsPanel.uiFont.default', stack: null },
  { key: 'system', labelKey: 'settingsPanel.uiFont.system', stack: 'system-ui, -apple-system, "Segoe UI", sans-serif' },
  { key: 'arial', label: 'Arial', stack: 'Arial, Helvetica, sans-serif' },
  { key: 'verdana', label: 'Verdana', stack: 'Verdana, Geneva, sans-serif' },
  { key: 'georgia', label: 'Georgia', stack: 'Georgia, serif' },
]

// monoFonts are the monospace choices for --font-mono (code blocks, the
// message source view, technical labels). default is the bundled Spline Sans
// Mono from tokens.css.
export const monoFonts: BodyFont[] = [
  { key: 'default', labelKey: 'settingsPanel.monoFont.default', stack: null },
  { key: 'system', labelKey: 'settingsPanel.monoFont.system', stack: 'ui-monospace, "SF Mono", Consolas, "Liberation Mono", monospace' },
  { key: 'courier', label: 'Courier New', stack: '"Courier New", Courier, monospace' },
]

// sysStack turns a "sys:<family>" key into a css stack over the given generic
// fallback, in case the font was uninstalled since it was chosen.
function sysStack(key: string, generic: string): string | null {
  const family = key.slice(4).replace(/["\\]/g, '')
  return family ? `"${family}", ${generic}` : null
}

// bodyFontStack maps a stored key to its css stack, or null for the default
// (and for any unknown key, so a removed entry degrades to the ui font).
// keys prefixed "sys:" name an installed system font family directly (the
// dropdown lists them from the ListSystemFonts binding); a generic fallback
// rides along in case the font was uninstalled since.
export function bodyFontStack(key: string): string | null {
  if (key.startsWith('sys:')) {
    return sysStack(key, 'sans-serif')
  }
  return bodyFonts.find((f) => f.key === key)?.stack ?? null
}

// uiFontStack / monoFontStack are the same lookup for the interface and
// monospace font settings; null means "keep the token's built-in value".
export function uiFontStack(key: string): string | null {
  if (key.startsWith('sys:')) {
    return sysStack(key, 'sans-serif')
  }
  return uiFonts.find((f) => f.key === key)?.stack ?? null
}

export function monoFontStack(key: string): string | null {
  if (key.startsWith('sys:')) {
    return sysStack(key, 'monospace')
  }
  return monoFonts.find((f) => f.key === key)?.stack ?? null
}
