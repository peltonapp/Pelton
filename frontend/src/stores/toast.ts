// toast.ts is a tiny ephemeral notification store for surfacing errors and
// confirmations (send queued, sync failed). it is ui-only state, so an in-memory
// store is the right home; nothing here is persisted.

import { writable } from 'svelte/store'

export type ToastKind = 'info' | 'success' | 'error'

// ToastAction is an optional inline button, used by the undo-send toast.
export interface ToastAction {
  label: string
  run: () => void
}

export interface Toast {
  id: number
  kind: ToastKind
  message: string
  action?: ToastAction
  /** More buttons after the action, such as Open beside Undo. */
  more?: ToastAction[]
  /** Work is still running: the toast shows a progress pie and stays. */
  busy?: boolean
}

export const toasts = writable<Toast[]>([])

let nextId = 1

// push adds a toast and auto-dismisses it after a delay. errors linger longer so
// the user can read them.
export function push(kind: ToastKind, message: string): void {
  const id = nextId++
  toasts.update((list) => [...list, { id, kind, message }])
  const ttl = kind === 'error' ? 6000 : 3000
  expireAfter(id, ttl)
}

interface Expiry {
  handle: ReturnType<typeof setTimeout> | null
  remaining: number
  started: number
}

const expiries = new Map<number, Expiry>()

function expireAfter(id: number, ttl: number): void {
  expiries.set(id, { handle: setTimeout(() => dismiss(id), ttl), remaining: ttl, started: Date.now() })
}

/** Holds a toast on screen while the pointer or focus is on it. */
export function pauseToast(id: number): void {
  const e = expiries.get(id)
  if (e?.handle) {
    clearTimeout(e.handle)
    e.handle = null
    e.remaining = Math.max(0, e.remaining - (Date.now() - e.started))
  }
}

/** Lets a held toast run out its remaining time, with a little added to read it again. */
export function resumeToast(id: number): void {
  const e = expiries.get(id)
  if (e && !e.handle) {
    e.remaining = Math.max(e.remaining, 1500)
    e.started = Date.now()
    e.handle = setTimeout(() => dismiss(id), e.remaining)
  }
}

// pushAction adds a toast carrying an inline action button and a caller-chosen
// ttl, returning its id so the caller can update or dismiss it (the undo-send
// countdown does both). it does not auto-dismiss when ttl is 0.
export function pushAction(kind: ToastKind, message: string, action: ToastAction, ttl: number): number {
  const id = nextId++
  toasts.update((list) => [...list, { id, kind, message, action }])
  if (ttl > 0) {
    expireAfter(id, ttl)
  }
  return id
}

// pushActions adds a toast with several buttons; each one also dismisses it.
export function pushActions(kind: ToastKind, message: string, actions: ToastAction[], ttl: number): number {
  const id = nextId++
  const wrap = (a: ToastAction): ToastAction => ({ label: a.label, run: () => (dismiss(id), a.run()) })
  const [first, ...rest] = actions.map(wrap)
  toasts.update((list) => [...list, { id, kind, message, action: first, more: rest }])
  if (ttl > 0) {
    expireAfter(id, ttl)
  }
  return id
}

const busyShowMs = 350

/**
 * Runs work that can take a while under a toast with a progress pie. The
 * toast appears only once the work has run for a moment, so a quick job or a
 * cancelled file dialog shows nothing, and goes when the work ends; reporting
 * the outcome stays with the caller.
 */
export async function withBusyToast<T>(message: string, work: () => Promise<T>): Promise<T> {
  let id = 0
  const timer = setTimeout(() => {
    id = nextId++
    toasts.update((list) => [...list, { id, kind: 'info', message, busy: true }])
  }, busyShowMs)
  try {
    return await work()
  } finally {
    clearTimeout(timer)
    if (id !== 0) {
      dismiss(id)
    }
  }
}

// updateToast replaces the message of a live toast, used to tick the countdown.
export function updateToast(id: number, message: string): void {
  toasts.update((list) => list.map((t) => (t.id === id ? { ...t, message } : t)))
}

export function dismiss(id: number): void {
  const e = expiries.get(id)
  if (e?.handle) {
    clearTimeout(e.handle)
  }
  expiries.delete(id)
  toasts.update((list) => list.filter((t) => t.id !== id))
}

// helpers for the common cases.
export const toastError = (message: string): void => push('error', message)
export const toastSuccess = (message: string): void => push('success', message)
export const toastInfo = (message: string): void => push('info', message)

// errorMessage normalizes an unknown thrown value into a string. wails rejects
// bound calls with an Error or a string; this handles both.
export function errorMessage(err: unknown): string {
  if (err instanceof Error) {
    return err.message
  }
  if (typeof err === 'string') {
    return err
  }
  return 'Something went wrong'
}
