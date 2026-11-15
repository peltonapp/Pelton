// focus.ts (store) mirrors focus mode from the backend, which owns it: whether
// it is on, when it ends, and the summary toast when it does. The top bar
// indicator, the palette and the shortcut all go through here.

import { get, writable } from 'svelte/store'
import { endFocus, getFocus, startFocus } from '../lib/api'
import { onFocusChanged, onFocusEnded, type Unsubscribe } from '../lib/events'
import { t } from '../lib/i18n'
import type { FocusInfo, FocusSummary } from '../lib/types'
import { errorMessage, pushAction, toastError, toastInfo } from './toast'
import { selectView, unifiedViewLabel } from './selection'
import { openSpace } from './space'

/** Focus mode as the backend last reported it. */
export const focus = writable<FocusInfo>({ active: false, until: '', scheduled: false, held: 0 })

/** The top bar's focus menu is open; its shortcut toggles it. */
export const focusMenuOpen = writable(false)

/** How long the summary toast stays. */
const summaryMs = 10000

let endTimer: ReturnType<typeof setTimeout> | undefined

// the backend's ticker ends focus within half a minute; asking it at the end
// time itself makes the indicator and the summary arrive on time.
function apply(info: FocusInfo): void {
  focus.set(info)
  clearTimeout(endTimer)
  const end = info.active && info.until ? Date.parse(info.until) : NaN
  if (!Number.isNaN(end)) {
    endTimer = setTimeout(() => void refreshFocus(), Math.max(0, end - Date.now()) + 500)
  }
}

/** Reads focus mode from the backend again. */
export async function refreshFocus(): Promise<void> {
  try {
    apply(await getFocus())
  } catch {
    // keep what is shown.
  }
}

/** Words what arrived during focus, as the toast says it. */
export function summaryText(sum: FocusSummary, translate: (key: string) => string): string {
  if (sum.total === 0) {
    return translate('focus.ended.nothing')
  }
  const count =
    sum.total === 1 ? translate('focus.ended.one') : translate('focus.ended.many').replace('{n}', String(sum.total))
  if (sum.vip === 0) {
    return translate('focus.ended.title').replace('{summary}', count)
  }
  const vip = sum.vip === 1 ? translate('focus.ended.vipOne') : translate('focus.ended.vipMany').replace('{n}', String(sum.vip))
  return translate('focus.ended.title').replace('{summary}', translate('focus.ended.withVip').replace('{count}', count).replace('{vip}', vip))
}

function showArrived(): void {
  openSpace('mail')
  selectView('unread', unifiedViewLabel('unread'))
}

function announce(sum: FocusSummary): void {
  const translate = get(t)
  const text = summaryText(sum, translate)
  if (sum.total === 0) {
    toastInfo(text)
    return
  }
  pushAction('info', text, { label: translate('focus.ended.show'), run: showArrived }, summaryMs)
}

/** Loads focus mode and follows its changes. Returns the unsubscribe. */
export async function initFocus(): Promise<Unsubscribe> {
  const offChanged = onFocusChanged(apply)
  const offEnded = onFocusEnded(announce)
  await refreshFocus()
  return () => {
    offChanged()
    offEnded()
    clearTimeout(endTimer)
  }
}

/** Turns focus on until the given time, or until it is turned off with null. */
export async function beginFocus(until: Date | null): Promise<void> {
  try {
    apply(await startFocus(until))
  } catch (err) {
    toastError(errorMessage(err))
  }
}

/** Turns focus off now. */
export async function finishFocus(): Promise<void> {
  try {
    await endFocus()
    await refreshFocus()
  } catch (err) {
    toastError(errorMessage(err))
  }
}
