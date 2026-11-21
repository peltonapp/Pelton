// flagurl.dev.ts stands in for flagurl.ts on the dev server. The build's
// template becomes an import of every flag there, 271 requests the moment the
// language picker loads, and fired at once through the wails asset proxy they
// overflowed the dev server's connection queue: the settings screen failed to
// load. A plain path is fetched only when a flag is actually shown.

/** The url of one flag by lowercase ISO 3166-1 code. */
export function flagUrl(code: string): string {
  return `/node_modules/flag-icons/flags/4x3/${code}.svg`
}
