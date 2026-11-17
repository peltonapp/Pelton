// senderreading.ts holds what the reader remembers per sender: whether html
// mail from them adapts to a dark theme or keeps its colours, and the text
// size it was set to for them. Both live in the local database and are read
// once, the first time the reader asks. A colour choice made for one message
// lasts for the session only, and whether the theme on screen is dark is
// tracked here too, since that is what the colour choice depends on.

import { get, readable, writable } from 'svelte/store'
import { listSenderReading, resetSenderReading, setSenderColors, setSenderFontSize } from '../lib/api'
import type { ColorMode } from '../lib/mailcolors'
import { bareAddress } from './vip'
import { errorMessage, toastError } from './toast'

/** One sender's remembered choices; '' and 0 follow the global settings. */
export interface SenderChoice {
  colors: '' | ColorMode
  fontSize: number
}

/** The remembered choices, by bare lower case address. */
export const senderReading = writable<Map<string, SenderChoice>>(new Map())

/** Colour choices made for single messages this session, by message id. */
export const messageColors = writable<Map<number, ColorMode>>(new Map())

let loading: Promise<void> | null = null

/** The key a sender is remembered under. */
export function senderKey(address: string): string {
  return bareAddress(address).toLowerCase()
}

/** Reads the remembered choices once; later calls wait for the same read. */
export function ensureSenderReading(): Promise<void> {
  if (!loading) {
    loading = listSenderReading()
      .then((rows) => {
        senderReading.set(new Map(rows.map((r) => [r.address, { colors: r.colors, fontSize: r.fontSize }])))
      })
      .catch((err) => {
        loading = null
        toastError(errorMessage(err))
      })
  }
  return loading
}

/** The choices remembered for a sender, or the defaults. */
export function choiceFor(map: Map<string, SenderChoice>, address: string): SenderChoice {
  return map.get(senderKey(address)) ?? { colors: '', fontSize: 0 }
}

function put(address: string, change: Partial<SenderChoice>): SenderChoice {
  const key = senderKey(address)
  const next = { ...choiceFor(get(senderReading), address), ...change }
  senderReading.update((m) => {
    const copy = new Map(m)
    if (next.colors === '' && next.fontSize === 0) {
      copy.delete(key)
    } else {
      copy.set(key, next)
    }
    return copy
  })
  return next
}

/** Remembers how html mail from a sender is coloured; '' forgets it. */
export async function rememberColors(address: string, colors: '' | ColorMode): Promise<void> {
  const before = choiceFor(get(senderReading), address)
  put(address, { colors })
  try {
    await setSenderColors(senderKey(address), colors)
  } catch (err) {
    put(address, { colors: before.colors })
    toastError(errorMessage(err))
  }
}

/** Remembers the reader text size for a sender; 0 forgets it. */
export async function rememberFontSize(address: string, fontSize: number): Promise<void> {
  const before = choiceFor(get(senderReading), address)
  put(address, { fontSize })
  try {
    await setSenderFontSize(senderKey(address), fontSize)
  } catch (err) {
    put(address, { fontSize: before.fontSize })
    toastError(errorMessage(err))
  }
}

/** Sets or clears the colour choice for one message, for this session. */
export function setMessageColors(id: number, mode: ColorMode | ''): void {
  messageColors.update((m) => {
    const copy = new Map(m)
    if (mode === '') {
      copy.delete(id)
    } else {
      copy.set(id, mode)
    }
    return copy
  })
}

/** Forgets every remembered sender choice, and says how many senders there were. */
export async function forgetSenderReading(): Promise<number> {
  const n = await resetSenderReading()
  senderReading.set(new Map())
  messageColors.set(new Map())
  return n
}

/** Whether the theme on screen is dark, following the root's data-theme. */
export const darkTheme = readable(false, (set) => {
  const root = document.documentElement
  const read = (): void => set(root.getAttribute('data-theme') === 'dark')
  read()
  const observer = new MutationObserver(read)
  observer.observe(root, { attributes: true, attributeFilter: ['data-theme'] })
  return () => observer.disconnect()
})
