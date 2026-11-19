// locations.ts describes where in the app the user is, for going back and
// forward. a location is a space plus what is open in it; every detail is
// optional, so the bare space is a location too. each space registers a
// provider that reads its current location from its own stores and shows a
// stored one again, which keeps the history store free of space imports and
// keeps a space without a provider working at the space level.

import type { Readable } from 'svelte/store'
import type { CalendarView, FeedScope, Space, ViewKey } from './types'

/** What the mail list shows. */
export type MailList =
  | { kind: 'view'; view: ViewKey }
  | { kind: 'folder'; folderId: number; accountId: number }
  | { kind: 'savedView'; viewId: number }

/** A list in Mail and the conversation open in it. */
export interface MailLocation {
  space: 'mail'
  list?: MailList
  message?: number
}

/** A date in the calendar, as yyyy-mm-dd, and the view it is shown in. */
export interface CalendarLocation {
  space: 'calendar'
  view?: CalendarView
  date?: string
}

/** The task open in Tasks. */
export interface TasksLocation {
  space: 'tasks'
  task?: number
}

/** A project, the tab it is on and the card open on its board. */
export interface ProjectsLocation {
  space: 'projects'
  project?: number
  tab?: string
  card?: string
}

/** The contact open in Contacts. */
export interface ContactsLocation {
  space: 'contacts'
  contact?: number
}

/** The note open in Notes, by its path in the vault. */
export interface NotesLocation {
  space: 'notes'
  note?: string
}

/** What the feeds list shows and the row open in the reader. */
export interface FeedsLocation {
  space: 'feeds'
  scope?: FeedScope
  entry?: string
}

/** The room open in Chat. */
export interface ChatLocation {
  space: 'chat'
  room?: string
}

/** Each space's location type. */
export interface LocationMap {
  mail: MailLocation
  calendar: CalendarLocation
  tasks: TasksLocation
  projects: ProjectsLocation
  contacts: ContactsLocation
  notes: NotesLocation
  feeds: FeedsLocation
  chat: ChatLocation
}

/** A place in the app. */
export type Location = LocationMap[Space]

/** How a space takes part in the history. */
export interface LocationProvider<S extends Space> {
  /** The stores whose change can move the space's location or its label. */
  watch: Readable<unknown>[]
  /** Where the space is now. */
  describe(): LocationMap[S]
  /** A readable name for what the location shows, or empty for the space alone. */
  label(loc: LocationMap[S]): string
  /** Shows the location; false when what it names no longer exists. */
  restore(loc: LocationMap[S]): Promise<boolean>
  /**
   * Whether moving from one location to the next only opens or closes a
   * detail in place, so it replaces the history entry instead of adding one.
   */
  refines?(from: LocationMap[S], to: LocationMap[S]): boolean
}

type AnyProvider = LocationProvider<Space>

const providers = new Map<Space, AnyProvider>()
const listeners = new Set<(provider: AnyProvider) => void>()

/** Registers how a space describes and restores its location; again replaces it. */
export function registerLocationProvider<S extends Space>(space: S, provider: LocationProvider<S>): void {
  const p = provider as unknown as AnyProvider
  providers.set(space, p)
  for (const listen of listeners) {
    listen(p)
  }
}

/** The provider of a space, if it registered one. */
export function locationProvider(space: Space): AnyProvider | undefined {
  return providers.get(space)
}

/** Every registered provider. */
export function locationProviders(): AnyProvider[] {
  return [...providers.values()]
}

/** Calls back for each provider registered from now on; returns the way to stop. */
export function onProviderRegistered(listen: (provider: AnyProvider) => void): () => void {
  listeners.add(listen)
  return () => listeners.delete(listen)
}

// canonical turns a value into JSON with its object keys sorted, so two
// locations with the same content always give the same key.
function canonical(value: unknown): string {
  if (Array.isArray(value)) {
    return `[${value.map(canonical).join(',')}]`
  }
  if (value && typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>)
      .filter(([, v]) => v !== undefined && v !== null)
      .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    return `{${entries.map(([k, v]) => `${JSON.stringify(k)}:${canonical(v)}`).join(',')}}`
  }
  return JSON.stringify(value)
}

/** A key that is equal for two locations exactly when they name the same place. */
export function locationKey(loc: Location): string {
  return canonical(loc)
}

/** The local calendar date of a time, as yyyy-mm-dd. */
export function ymd(date: Date): string {
  const pad = (n: number): string => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

/** The local midnight of a yyyy-mm-dd date, or null for anything else. */
export function parseYmd(text: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(text)
  if (!m) {
    return null
  }
  const date = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
  return Number.isNaN(date.getTime()) || ymd(date) !== text ? null : date
}

/** Whether one of two details is empty, so the move opens or closes it in place. */
export function oneSideEmpty(a: unknown, b: unknown): boolean {
  return a === undefined || b === undefined
}
