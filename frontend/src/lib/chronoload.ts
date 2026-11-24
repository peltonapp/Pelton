// chronoload.ts loads the chrono date parser on first use, so it costs nothing
// at startup: the parser of the app language where chrono has one, with the
// English one behind it for what the first does not read.

import type { ChronoLike } from './eventphrase'

type Loader = () => Promise<{ casual: ChronoLike }>

const loaders: Record<string, Loader> = {
  en: () => import('chrono-node/en'),
  de: () => import('chrono-node/de'),
  fr: () => import('chrono-node/fr'),
  nl: () => import('chrono-node/nl'),
  es: () => import('chrono-node/es'),
  it: () => import('chrono-node/it'),
  pt: () => import('chrono-node/pt'),
  ru: () => import('chrono-node/ru'),
  zh: () => import('chrono-node/zh'),
  ar: () => import('chrono-node/ar'),
}

const cache = new Map<string, Promise<ChronoLike[]>>()

/** The parsers to read a line with in the given app language, English last. */
export function loadChrono(lang: string): Promise<ChronoLike[]> {
  const key = lang.toLowerCase().split(/[-_]/)[0]
  let pending = cache.get(key)
  if (!pending) {
    const own = key !== 'en' && Object.hasOwn(loaders, key) ? loaders[key] : null
    const chain = own ? [own, loaders.en] : [loaders.en]
    pending = Promise.all(chain.map((load) => load().then((mod) => mod.casual)))
    pending.catch(() => cache.delete(key))
    cache.set(key, pending)
  }
  return pending
}
