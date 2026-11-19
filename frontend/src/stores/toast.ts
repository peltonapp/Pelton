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
}

export const toasts = writable<Toast[]>([])

let nextId = 1

// push adds a toast and auto-dismisses it after a delay. errors linger longer so
// the user can read them.
export function push(kind: ToastKind, message: string): void {
  const id = nextId++
  toasts.update((list) => [...list, { id, kind, message }])
  const ttl = kind === 'error' ? 6000 : 3000
  setTimeout(() => dismiss(id), ttl)
}

// pushAction adds a toast carrying an inline action button and a caller-chosen
// ttl, returning its id so the caller can update or dismiss it (the undo-send
// countdown does both). it does not auto-dismiss when ttl is 0.
export function pushAction(kind: ToastKind, message: string, action: ToastAction, ttl: number): number {
  const id = nextId++
  toasts.update((list) => [...list, { id, kind, message, action }])
  if (ttl > 0) {
    setTimeout(() => dismiss(id), ttl)
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
    setTimeout(() => dismiss(id), ttl)
  }
  return id
}

// updateToast replaces the message of a live toast, used to tick the countdown.
export function updateToast(id: number, message: string): void {
  toasts.update((list) => list.map((t) => (t.id === id ? { ...t, message } : t)))
}

export function dismiss(id: number): void {
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
