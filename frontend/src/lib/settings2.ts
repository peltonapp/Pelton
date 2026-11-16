// settings2.ts is the map of the new settings screen: its pages, the groups
// they sit under, and the search index over single settings. each page is a
// component in components/settings2/pages named after its key, loaded when it
// is first opened; a page's searchable settings live beside it in
// <Key>.index.ts.

import type { ComponentType } from 'svelte'
import generalIcon from '@phosphor-icons/core/assets/duotone/sliders-horizontal-duotone.svg?raw'
import designIcon from '@phosphor-icons/core/assets/duotone/swatches-duotone.svg?raw'
import themeIcon from '@phosphor-icons/core/assets/duotone/palette-duotone.svg?raw'
import layoutIcon from '@phosphor-icons/core/assets/duotone/layout-duotone.svg?raw'
import motionIcon from '@phosphor-icons/core/assets/duotone/waves-duotone.svg?raw'
import listIcon from '@phosphor-icons/core/assets/duotone/list-dashes-duotone.svg?raw'
import readingIcon from '@phosphor-icons/core/assets/duotone/book-open-text-duotone.svg?raw'
import composingIcon from '@phosphor-icons/core/assets/duotone/pencil-simple-line-duotone.svg?raw'
import signaturesIcon from '@phosphor-icons/core/assets/duotone/signature-duotone.svg?raw'
import snippetsIcon from '@phosphor-icons/core/assets/duotone/text-indent-duotone.svg?raw'
import contactsIcon from '@phosphor-icons/core/assets/duotone/address-book-duotone.svg?raw'
import calendarIcon from '@phosphor-icons/core/assets/duotone/calendar-blank-duotone.svg?raw'
import tasksIcon from '@phosphor-icons/core/assets/duotone/list-checks-duotone.svg?raw'
import notesIcon from '@phosphor-icons/core/assets/duotone/notebook-duotone.svg?raw'
import feedsIcon from '@phosphor-icons/core/assets/duotone/rss-duotone.svg?raw'
import accountsIcon from '@phosphor-icons/core/assets/duotone/at-duotone.svg?raw'
import vacationIcon from '@phosphor-icons/core/assets/duotone/airplane-takeoff-duotone.svg?raw'
import filtersIcon from '@phosphor-icons/core/assets/duotone/funnel-duotone.svg?raw'
import profilesIcon from '@phosphor-icons/core/assets/duotone/users-duotone.svg?raw'
import backupIcon from '@phosphor-icons/core/assets/duotone/archive-duotone.svg?raw'
import privacyIcon from '@phosphor-icons/core/assets/duotone/shield-check-duotone.svg?raw'
import encryptionIcon from '@phosphor-icons/core/assets/duotone/key-duotone.svg?raw'
import notificationsIcon from '@phosphor-icons/core/assets/duotone/bell-duotone.svg?raw'
import insightsIcon from '@phosphor-icons/core/assets/duotone/chart-bar-duotone.svg?raw'
import shortcutsIcon from '@phosphor-icons/core/assets/duotone/keyboard-duotone.svg?raw'
import syncIcon from '@phosphor-icons/core/assets/duotone/arrows-clockwise-duotone.svg?raw'
import aboutIcon from '@phosphor-icons/core/assets/duotone/info-duotone.svg?raw'

/** One page of the settings screen. */
export interface SettingsPage {
  /** Stable key; also the name of the page's component file, capitalized. */
  key: string
  /** Key of the group it sits under, or '' for a page listed on its own. */
  group: string
  /** Localization key of its name. */
  labelKey: string
  /** Raw Phosphor svg. */
  icon: string
}

/** One heading in the settings navigation. */
export interface SettingsGroup {
  key: string
  labelKey: string
}

/** One setting the search can jump to. */
export interface SettingsIndexEntry {
  /** Key of the page holding it. */
  page: string
  /** The `id` of its Row on that page. */
  id: string
  /** Localization key of its name. */
  labelKey: string
  /** English synonyms, space separated. */
  keywords?: string
}

/** The navigation groups, in display order. */
export const settingsGroups2: SettingsGroup[] = [
  { key: 'appearance', labelKey: 'settings2.group.appearance' },
  { key: 'mail', labelKey: 'settings2.group.mail' },
  { key: 'people', labelKey: 'settings2.group.people' },
  { key: 'accounts', labelKey: 'settings2.group.accounts' },
  { key: 'privacy', labelKey: 'settings2.group.privacy' },
  { key: 'system', labelKey: 'settings2.group.system' },
]

/** Every page, in display order. Pages with group '' come first. */
export const settingsPages2: SettingsPage[] = [
  { key: 'general', group: '', labelKey: 'settings2.page.general', icon: generalIcon },
  { key: 'design', group: 'appearance', labelKey: 'settings2.page.design', icon: designIcon },
  { key: 'theme', group: 'appearance', labelKey: 'settings2.page.theme', icon: themeIcon },
  { key: 'layout', group: 'appearance', labelKey: 'settings2.page.layout', icon: layoutIcon },
  { key: 'motion', group: 'appearance', labelKey: 'settings2.page.motion', icon: motionIcon },
  { key: 'list', group: 'mail', labelKey: 'settings2.page.list', icon: listIcon },
  { key: 'reading', group: 'mail', labelKey: 'settings2.page.reading', icon: readingIcon },
  { key: 'composing', group: 'mail', labelKey: 'settings2.page.composing', icon: composingIcon },
  { key: 'signatures', group: 'mail', labelKey: 'settings2.page.signatures', icon: signaturesIcon },
  { key: 'snippets', group: 'mail', labelKey: 'settings2.page.snippets', icon: snippetsIcon },
  { key: 'notifications', group: 'mail', labelKey: 'settings2.page.notifications', icon: notificationsIcon },
  { key: 'calendar', group: 'people', labelKey: 'settings2.page.calendar', icon: calendarIcon },
  { key: 'tasks', group: 'people', labelKey: 'settings2.page.tasks', icon: tasksIcon },
  { key: 'notes', group: 'people', labelKey: 'settings2.page.notes', icon: notesIcon },
  { key: 'feeds', group: 'people', labelKey: 'settings2.page.feeds', icon: feedsIcon },
  { key: 'insights', group: 'mail', labelKey: 'settings2.page.insights', icon: insightsIcon },
  { key: 'contacts', group: 'people', labelKey: 'settings2.page.contacts', icon: contactsIcon },
  { key: 'accounts', group: 'accounts', labelKey: 'settings2.page.accounts', icon: accountsIcon },
  { key: 'vacation', group: 'accounts', labelKey: 'settings2.page.vacation', icon: vacationIcon },
  { key: 'filters', group: 'accounts', labelKey: 'settings2.page.filters', icon: filtersIcon },
  { key: 'profiles', group: 'accounts', labelKey: 'settings2.page.profiles', icon: profilesIcon },
  { key: 'backup', group: 'accounts', labelKey: 'settings2.page.backup', icon: backupIcon },
  { key: 'privacy', group: 'privacy', labelKey: 'settings2.page.privacy', icon: privacyIcon },
  { key: 'encryption', group: 'privacy', labelKey: 'settings2.page.encryption', icon: encryptionIcon },
  { key: 'shortcuts', group: 'system', labelKey: 'settings2.page.shortcuts', icon: shortcutsIcon },
  { key: 'sync', group: 'system', labelKey: 'settings2.page.sync', icon: syncIcon },
  { key: 'about', group: 'system', labelKey: 'settings2.page.about', icon: aboutIcon },
]

// category keys of the classic settings panel, which menu actions and the
// command palette still deep-link by, mapped to the page that holds them now.
const classicKeys: Record<string, string> = {
  appearance: 'theme',
  themes: 'theme',
  menubar: 'layout',
  language: 'general',
  list: 'list',
  display: 'reading',
  composing: 'composing',
  signatures: 'signatures',
  notifications: 'notifications',
  gestures: 'list',
  privacy: 'privacy',
  encryption: 'encryption',
  mailboxes: 'accounts',
  contacts: 'contacts',
  profiles: 'profiles',
  external: 'backup',
  power: 'sync',
  shortcuts: 'shortcuts',
  about: 'about',
}

const loaders = import.meta.glob<{ default: ComponentType }>('../components/settings2/pages/*.svelte')
const indexes = import.meta.glob<{ default: SettingsIndexEntry[] }>('../components/settings2/pages/*.index.ts', { eager: true })

function pagePath(key: string): string {
  return `../components/settings2/pages/${key.charAt(0).toUpperCase()}${key.slice(1)}.svelte`
}

/** Says whether the page has a component to show. */
export function pageExists(key: string): boolean {
  return pagePath(key) in loaders
}

/** Loads the component of a page. */
export async function loadPage(key: string): Promise<ComponentType> {
  const module = await loaders[pagePath(key)]()
  return module.default
}

/** Resolves a page key or a classic category key to a page that exists. */
export function resolvePage(key: string | null): string {
  const wanted = key ? (classicKeys[key] ?? key) : 'general'
  if (pageExists(wanted)) {
    return wanted
  }
  return settingsPages2.find((p) => pageExists(p.key))?.key ?? 'general'
}

/** Every searchable setting of every page. */
export const settingsIndex2: SettingsIndexEntry[] = Object.values(indexes).flatMap((m) => m.default)
