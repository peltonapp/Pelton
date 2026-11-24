// i18n.ts is a small, dependency-free localization layer. it exposes a
// reactive locale store (backed by the settings table, like every other
// preference), a t() translation store for looking up strings, and the
// platform-correct modifier symbol so keyboard shortcut hints read natively
// (cmd on macos, ctrl elsewhere).
//
// coverage note: this catalogs the app's chrome (menus, shortcuts, common
// settings labels and actions, onboarding) rather than literally every string
// in the app. that is a deliberate scope choice: exhaustively translating
// every screen in one pass would be both huge and risky to review, whereas
// the catalog here is additive and any component can adopt t() incrementally.
//
// each language lives in its own file under lib/locales/, which the build
// splits into the areas of stringareas.ts. english core strings are bundled,
// since english is the always-available fallback; every other area loads with
// the code that shows it (loadStrings, used through lazyload.ts), and another
// language loads only once the user selects it.
//
// on top of the built-ins, custom languages live as json files in the app's
// locales folder (see the backend's bind_locales.go). the language setting
// stores them as "user:<id>"; their strings resolve first, then the base
// language the file declares, then english. a file with only a few strings
// is a per-string override on top of its base.

import { writable, derived, get } from 'svelte/store'
import * as english from 'virtual:strings/en'
import { areaOf, type StringArea } from './stringareas'
import { getUserLocale } from './api'
import { applyCJK, applyDirection } from '../theme/theme'
import { isMac, isWindows } from './platform'

export type Locale = 'en' | 'de' | 'fr' | 'nl' | 'es' | 'it' | 'pl' | 'ru' | 'tr' | 'pt' | 'ar' | 'zh-CN'

export const locales: Locale[] = ['en', 'de', 'fr', 'nl', 'es', 'it', 'pl', 'ru', 'tr', 'pt', 'ar', 'zh-CN']

/** Text direction of the interface. */
export type Direction = 'ltr' | 'rtl'

// which languages read right to left (#356). Only the exceptions are listed,
// since every other locale is left to right and a missing entry is the common
// case rather than an oversight.
const rtlLocales = new Set<Locale>(['ar'])

/** The direction a language is written in. */
export function directionOf(l: Locale): Direction {
  return rtlLocales.has(l) ? 'rtl' : 'ltr'
}

// which languages are written in Han characters, which the interface font has
// no glyphs for. Listed separately from rtlLocales because the script a
// language uses and the direction it runs in are different questions: Arabic
// happens to answer both at once, Chinese only the first.
const cjkLocales = new Set<Locale>(['zh-CN'])

// each language is shown in its own spelling, not translated into the
// currently active one, so it stays recognizable no matter what is selected.
export const localeNames: Record<Locale, string> = {
  en: 'English',
  de: 'Deutsch',
  fr: 'Français',
  nl: 'Nederlands',
  es: 'Español',
  it: 'Italiano',
  pl: 'Polski',
  ru: 'Русский',
  tr: 'Türkçe',
  // qualified because the catalog is European Portuguese, not pt-BR:
  // ficheiro/gerir/ecrã rather than arquivo/gerenciar/tela.
  pt: 'Português (Portugal)',
  ar: 'العربية',
  // written in the simplified characters used in mainland China, not the
  // traditional ones, so the code names the region rather than the language.
  'zh-CN': '简体中文',
}

type Strings = Record<string, string>

/** A language's strings: core inline, every other area behind a loader. */
type LocaleStrings = { core: Strings; areas: Record<string, () => Promise<{ default: Strings }>> }

const loaders: Record<Exclude<Locale, 'en'>, () => Promise<LocaleStrings>> = {
  de: () => import('virtual:strings/de'),
  fr: () => import('virtual:strings/fr'),
  nl: () => import('virtual:strings/nl'),
  es: () => import('virtual:strings/es'),
  it: () => import('virtual:strings/it'),
  pl: () => import('virtual:strings/pl'),
  ru: () => import('virtual:strings/ru'),
  tr: () => import('virtual:strings/tr'),
  pt: () => import('virtual:strings/pt'),
  ar: () => import('virtual:strings/ar'),
  'zh-CN': () => import('virtual:strings/zh-CN'),
}

// catalogs holds every locale's strings that have been loaded so far, merged
// across areas. english core is present from the start; fill adds the rest.
const catalogs = writable<Partial<Record<Locale, Strings>>>({ en: english.core })

// the areas this window has asked for, which every language it switches to
// loads as well.
const wanted = new Set<StringArea>(['core'])

// per "<locale>/<area>", the strings on their way, and the ones already merged.
const fetching = new Map<string, Promise<Strings>>()
const merged = new Set<string>(['en/core'])

function fetchArea(l: Locale, area: StringArea): Promise<Strings> {
  const id = `${l}/${area}`
  let strings = fetching.get(id)
  if (!strings) {
    strings = (async () => {
      const mod = l === 'en' ? english : await loaders[l]()
      if (area === 'core') {
        return mod.core
      }
      const load = mod.areas[area]
      return load ? (await load()).default : {}
    })()
    strings.catch(() => fetching.delete(id))
    fetching.set(id, strings)
  }
  return strings
}

// fill loads the given areas of the given languages and merges them in one
// store update, so text that needs several of them appears all at once.
async function fill(langs: Locale[], areas: StringArea[]): Promise<void> {
  const jobs: Promise<[Locale, string, Strings]>[] = []
  for (const l of langs) {
    for (const area of areas) {
      const id = `${l}/${area}`
      if (!merged.has(id)) {
        jobs.push(fetchArea(l, area).then((s) => [l, id, s]))
      }
    }
  }
  if (jobs.length === 0) {
    return
  }
  const done = await Promise.all(jobs)
  catalogs.update((c) => {
    const next = { ...c }
    for (const [l, id, s] of done) {
      if (!merged.has(id)) {
        merged.add(id)
        next[l] = { ...next[l], ...s }
      }
    }
    return next
  })
}

/**
 * loadStrings loads the string areas a part of the interface shows, in English
 * and in the active language, resolving once both are in.
 */
export function loadStrings(areas: StringArea[]): Promise<void> {
  for (const area of areas) {
    wanted.add(area)
  }
  const l = get(locale)
  return fill(l === 'en' ? ['en'] : ['en', l], areas)
}

async function ensureLoaded(l: Locale): Promise<void> {
  await fill([l], [...wanted])
}

// unloaded answers a key no loaded catalog has. One whose area nothing has
// asked for yet means a component shows text it did not declare: the area is
// loaded so the text still arrives, and the dev server says so. Tests carry
// all English text as core and check the areas statically (strings.test.ts).
function unloaded(key: string): string {
  const area = areaOf(key)
  if (area && !wanted.has(area)) {
    if (import.meta.env.DEV && import.meta.env.MODE !== 'test') {
      console.error(`i18n: "${key}" was shown before the ${area} strings were loaded`)
    }
    void loadStrings([area]).catch(() => undefined)
  }
  return key
}

// detectOSLocale reads the browser/OS language, used only to mark a
// "Recommended" option in the picker. it is never used to silently pick the
// active language: first run always defaults to English, and after that the
// user's own choice (persisted via settings) always wins.
export function detectOSLocale(): Locale {
  const tag = (navigator.language || 'en').toLowerCase()
  if (writesSimplifiedChinese(tag)) {
    return 'zh-CN'
  }
  // the whole tag is tried before its bare language, so a region-qualified
  // locale (zh-CN, simplified) can be recommended without also recommending it
  // to every other region the language is written in.
  const exact = locales.find((l) => l.toLowerCase() === tag)
  if (exact) return exact
  const base = tag.slice(0, 2)
  return locales.find((l) => l.toLowerCase() === base) ?? 'en'
}

// whether an operating system language tag names Chinese in simplified
// characters.
//
// Chinese needs its own answer because the tag alone does not give one. The
// three webviews do not agree on how they spell it: "zh-CN" from one,
// "zh-Hans-CN" from another, sometimes a bare "zh", and matching on the whole
// tag or its first two letters catches the first of those and misses the rest.
//
// Traditional is excluded rather than mapped. Simplified and traditional are
// not the same text to read, so a reader of traditional Chinese is better
// recommended English than a catalogue they would have to work through.
function writesSimplifiedChinese(tag: string): boolean {
  if (tag !== 'zh' && !tag.startsWith('zh-')) {
    return false
  }
  return !/(^|-)(hant|tw|hk|mo)(-|$)/.test(tag)
}

// the active locale. initPrefs (stores/prefs.ts) sets this from the persisted
// "language" setting on startup; the default here is only what renders before
// that first load resolves. while a custom language is active, this holds its
// base language so the fallback chain stays a plain catalog lookup.
export const locale = writable<Locale>('en')

// the interface follows the active language's direction, wherever that language
// came from: a built-in, or a custom file whose base is right-to-left. Hooking
// the store rather than each setter means no path can set a language and forget
// to turn the layout round with it.
locale.subscribe((l) => {
  const dir = directionOf(l)
  // lang matters beyond direction: it is what the webview uses to pick a face
  // for characters shared between scripts, and what a screen reader reads with.
  document.documentElement.lang = l
  void applyDirection(dir)
  void applyCJK(cjkLocales.has(l))
})

// the active custom language's strings, or null when a built-in is active.
const userCatalog = writable<Record<string, string> | null>(null)

// userLocalePrefix marks a custom language in the persisted language setting.
export const userLocalePrefix = 'user:'

// setLocale activates a language setting value: a built-in code, or
// "user:<id>" for a custom language file.
export function setLocale(value: string): void {
  if (value.startsWith(userLocalePrefix)) {
    void applyUserLocale(value.slice(userLocalePrefix.length))
    return
  }
  userCatalog.set(null)
  const l = (locales as string[]).includes(value) ? (value as Locale) : 'en'
  locale.set(l)
  void ensureLoaded(l)
}

// applyUserLocale loads a custom language from the backend and activates it
// over its base. a missing or broken file falls back to english; the stored
// selection stays, so fixing the file brings it back.
async function applyUserLocale(id: string): Promise<void> {
  try {
    const data = await getUserLocale(id)
    const base = (locales as string[]).includes(data.base) ? (data.base as Locale) : 'en'
    locale.set(base)
    await ensureLoaded(base)
    userCatalog.set(data.strings ?? {})
  } catch {
    userCatalog.set(null)
    locale.set('en')
  }
}

// t is reactive: components use $t('key') so the whole tree re-renders the
// instant the language changes (or its catalog finishes loading), with no
// reload required. while a locale's catalog is still loading, keys fall back
// to english until it arrives. a custom language resolves before its base.
export const t = derived([locale, catalogs, userCatalog], ([$locale, $catalogs, $user]) => (key: string): string => {
  return $user?.[key] ?? $catalogs[$locale]?.[key] ?? $catalogs.en?.[key] ?? unloaded(key)
})

export { isMac, isWindows }


// modSymbol is the display glyph for the primary modifier on this platform.
export const modSymbol = isMac ? '⌘' : 'Ctrl'

// shortcutLabel renders a combo like "mod+n" into a localized, platform-correct
// hint such as "⌘N" or "Ctrl+N". Shift in front of a symbol it produced, as
// in "shift+?", is left out: the symbol already says it.
export function shortcutLabel(combo: string): string {
  const parts = combo.split('+')
  const key = parts[parts.length - 1] ?? ''
  const shiftedSymbol = key.length === 1 && key.toLowerCase() === key.toUpperCase() && !/[0-9]/.test(key)
  return parts
    .filter((part) => !(shiftedSymbol && part === 'shift'))
    .map((part) => {
      if (part === 'mod') return modSymbol
      if (part === 'shift') return isMac ? '⇧' : 'Shift'
      if (part === 'alt') return isMac ? '⌥' : 'Alt'
      if (part === 'space') return 'Space'
      if (part === 'enter') return isMac ? '↩' : 'Enter'
      // the two delete keys have glyphs on macOS and names everywhere else.
      if (part === 'backspace') return isMac ? '⌫' : 'Backspace'
      if (part === 'delete') return isMac ? '⌦' : 'Delete'
      return part.length === 1 ? part.toUpperCase() : part
    })
    .join(isMac ? '' : '+')
}
