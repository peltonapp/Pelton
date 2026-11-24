// location-feeds.ts lets Feeds take part in back and forward: the list on
// show (a smart view, a feed or a folder) and the entry open in the reader.

import { get } from 'svelte/store'
import { getFeedEntry } from './api'
import { sameScope, scopeTitle } from './feedlogic'
import { registerLocationProvider, oneSideEmpty, type FeedsLocation } from './locations'
import { feedFocus, feedList, feedSelected, feedSidebar, showScope } from '../stores/feeds'
import { openSpace } from '../stores/space'
import { t } from './i18n'
import type { FeedScope } from './types'

/** The feed entry id of a reader row key, or null for a mail row or anything else. */
export function entryId(key: string | undefined): number | null {
  const m = /^e(\d+)$/.exec(key ?? '')
  return m ? Number(m[1]) : null
}

// whether a scope still names something: a feed or a folder can be removed.
function scopeExists(scope: FeedScope): boolean {
  const side = get(feedSidebar)
  if (scope.kind === 'feed') {
    return side.feeds.some((f) => f.id === scope.id)
  }
  if (scope.kind === 'folder') {
    return scope.id === 0 || side.folders.some((f) => f.id === scope.id)
  }
  return true
}

registerLocationProvider('feeds', {
  watch: [feedList, feedSelected, feedSidebar],
  describe(): FeedsLocation {
    const entry = get(feedSelected)
    return { space: 'feeds', scope: get(feedList).scope, entry: entry ?? undefined }
  },
  label(loc) {
    const row = loc.entry ? get(feedList).rows.find((r) => r.key === loc.entry) : undefined
    if (row?.title) {
      return row.title
    }
    return loc.scope ? scopeTitle(loc.scope, get(feedSidebar), get(t)) : ''
  },
  async restore(loc) {
    if (loc.scope && !scopeExists(loc.scope)) {
      return false
    }
    const id = entryId(loc.entry)
    if (id !== null) {
      try {
        await getFeedEntry(id)
      } catch {
        return false
      }
    }
    openSpace('feeds')
    if (loc.scope && !sameScope(get(feedList).scope, loc.scope)) {
      await showScope(loc.scope, false)
    }
    if (id !== null) {
      feedFocus.set(id)
    } else if (loc.entry === undefined) {
      feedSelected.set(null)
    }
    return true
  },
  refines(from, to) {
    return !!from.scope && !!to.scope && sameScope(from.scope, to.scope) && oneSideEmpty(from.entry, to.entry)
  },
})
