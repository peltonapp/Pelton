// insights.ts loads the sections of the insights page and keeps each result
// for the rest of the session, so switching ranges back and forth or reopening
// settings does not run the aggregates again. Everything is computed by the
// backend from the local store; nothing leaves the machine.

import { insightsPeople, insightsStorage, insightsVolume } from '../lib/api'
import { rangeDays, type InsightsPeople, type InsightsRange, type InsightsStorage, type InsightsVolume } from '../lib/mailstats'

const cache = new Map<string, Promise<unknown>>()

function cached<T>(key: string, load: () => Promise<T>): Promise<T> {
  const hit = cache.get(key)
  if (hit) {
    return hit as Promise<T>
  }
  const next = load()
  cache.set(key, next)
  // a failure is not kept, so trying again runs the query again.
  next.catch(() => cache.delete(key))
  return next
}

/** The storage section, read once a session. */
export function storageInsights(): Promise<InsightsStorage> {
  return cached('storage', insightsStorage)
}

/** The volume section for a range, read once a session per range. */
export function volumeInsights(range: InsightsRange): Promise<InsightsVolume> {
  return cached(`volume:${range}`, () => insightsVolume(rangeDays(range)))
}

/** The people section for a range, read once a session per range. */
export function peopleInsights(range: InsightsRange): Promise<InsightsPeople> {
  return cached(`people:${range}`, () => insightsPeople(rangeDays(range)))
}

/** Forgets every cached section, for the refresh button. */
export function clearInsights(): void {
  cache.clear()
}
