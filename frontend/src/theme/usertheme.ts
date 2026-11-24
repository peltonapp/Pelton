// usertheme.ts applies a custom theme (a validated ThemeApply from the
// backend) on top of the built-in token sets: one injected <style> element
// carrying the token overrides and the theme's css, the data-theme attribute
// pinned to the theme's base so unoverridden tokens fall through to the
// right built-in set, and the icon override registry. Removing the style
// element is a complete revert - the built-ins live in the bundled
// stylesheet, untouched.

import type { ThemeApply } from '../lib/types'
import { setIconOverrides } from './icons'
import { legibleTokens } from './legible'

// the id of the injected style element; one per document, replaced on every
// apply.
const styleId = 'pelton-user-theme'

// applyUserTheme injects the theme, or clears it when passed null. The
// token block selector (:root[data-theme]) ties specificity with the built-in
// dark selector, and the injected element sits after the bundled stylesheet,
// so theme tokens win exactly when present. The caller re-applies the normal
// theme preference after clearing (the base attribute stays behind
// otherwise).
export function applyUserTheme(theme: ThemeApply | null): void {
  document.getElementById(styleId)?.remove()
  if (!theme) {
    setIconOverrides(null)
    return
  }
  document.documentElement.setAttribute('data-theme', theme.base === 'dark' ? 'dark' : 'light')
  const style = document.createElement('style')
  style.id = styleId
  style.textContent = tokenBlock(theme.tokens) + legibleBlock(theme) + theme.css
  document.head.appendChild(style)
  setIconOverrides(theme.icons)
}

// hasUserTheme reports whether a custom theme is currently injected.
export function hasUserTheme(): boolean {
  return document.getElementById(styleId) !== null
}

// legibleBlock lifts the theme's text tokens to aa contrast in the new design
// only; the classic design keeps the theme's colors as they are.
function legibleBlock(theme: ThemeApply): string {
  const lifted = legibleTokens(theme.base === 'dark' ? 'dark' : 'light', theme.tokens)
  const entries = Object.entries(lifted)
  if (entries.length === 0) {
    return ''
  }
  const decls = entries.map(([name, value]) => `  --${name}: ${value};`)
  return `:root[data-design="new"][data-theme] {\n${decls.join('\n')}\n}\n`
}

// tokenBlock renders the override map as a css declaration block. Values were
// validated by the backend (allowlisted names, no declaration-escaping
// characters), so plain string assembly is safe here.
function tokenBlock(tokens: Record<string, string>): string {
  const entries = Object.entries(tokens)
  if (entries.length === 0) {
    return ''
  }
  const decls = entries.map(([name, value]) => `  --${name}: ${value};`)
  return `:root[data-theme] {\n${decls.join('\n')}\n}\n`
}
