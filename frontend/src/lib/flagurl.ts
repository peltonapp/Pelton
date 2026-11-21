// flagurl.ts is where a flag's url comes from in a build: vite reads the
// template, emits every svg it can match as an asset and maps the code to the
// hashed file. The dev server swaps in flagurl.dev.ts (see vite.config.ts),
// since there the same template turns into an import of all 271 flags at once.

/** The url of one flag by lowercase ISO 3166-1 code. */
export function flagUrl(code: string): string {
  return new URL(`../../node_modules/flag-icons/flags/4x3/${code}.svg`, import.meta.url).href
}
