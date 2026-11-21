// flags.ts resolves a language to the flag shown next to it in the picker.
//
// A language is not a country, so the mapping is data rather than something
// derivable: each locale carries the flag people actually associate with it.
// Where the language is spoken in several countries the operating system's own
// region settles it, read locally from navigator.language, so an en-US install
// gets the US flag and an en-GB one the union flag without asking anybody or
// touching the network.
//
// A language with no entry here gets no flag, and the picker shows a neutral
// mark in its place. It used to borrow whatever region the reader's machine
// named, which put Portugal's flag next to العربية for a reader in Portugal
// (#440): a flag nobody chose, attached to a language it has nothing to do
// with. Showing none says what is true.
//
// The svgs come from flag-icons (MIT) and are bundled, never fetched.

import { flagUrl } from './flagurl'

const regionNames = new Intl.DisplayNames(['en'], { type: 'region', fallback: 'none' })

// flag-icons carries a flag for every assigned ISO 3166-1 code, so a code the
// platform can name has one.
function hasFlag(code: string): boolean {
  return /^[a-z]{2}$/.test(code) && regionNames.of(code.toUpperCase()) !== undefined
}

// the flag a language falls back to when the operating system names no region,
// or names one the language is not spoken in. A language missing from here
// shows no flag at all.
const defaultCountry: Record<string, string> = {
  en: 'gb',
  de: 'de',
  fr: 'fr',
  nl: 'nl',
  es: 'es',
  it: 'it',
  pl: 'pl',
  ru: 'ru',
  tr: 'tr',
  pt: 'pt',
  // ar has no entry and is given none: no single country's flag stands for the
  // language, so the picker shows the neutral mark.
  // keyed by the full tag: a bare "zh" would also claim zh-TW, which uses a
  // different flag.
  'zh-cn': 'cn',
}

/**
 * The flag url for a language code, or undefined when there is nothing sensible
 * to show. Custom language files land here as undefined by design: an author's
 * own translation belongs to no country.
 */
export function flagFor(language: string): string | undefined {
  const tag = language.toLowerCase()
  // a locale that names its own region is read from that region alone: asking
  // the operating system would answer for whatever region the machine is in.
  if (tag.includes('-')) {
    const pinned = defaultCountry[tag]
    return pinned && hasFlag(pinned) ? flagUrl(pinned) : undefined
  }
  const country = osRegionFor(tag) ?? defaultCountry[tag]
  return country && hasFlag(country) ? flagUrl(country) : undefined
}

/**
 * The operating system's region, but only when it belongs to the language being
 * asked about. A de-DE install says nothing about which Spanish the user means,
 * so the Spanish tile keeps its default.
 */
function osRegionFor(language: string): string | undefined {
  const tag = typeof navigator === 'undefined' ? '' : navigator.language || ''
  const [base, region] = tag.toLowerCase().split('-')
  if (base !== language || !region || region.length !== 2) {
    return undefined
  }
  return hasFlag(region) ? region : undefined
}
