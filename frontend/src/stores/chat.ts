// chat.ts holds the chat space's state: the account, the room list, the
// timelines that have been opened, messages on their way out, who is typing
// and the device verifications in flight. It follows the backend's chat
// events while chat is on; startChat and stopChat bracket that.

import { derived, get, writable } from 'svelte/store'
import {
  chatStatus as fetchStatus,
  chatRooms as fetchRooms,
  chatTimeline,
  chatSend,
  chatReact,
  chatRedact,
  chatMarkRead,
  chatSetViewing,
  chatVerifications,
  chatSendFilePaths,
  chatSendFileData,
  chatTypists,
} from '../lib/api'
import {
  onChatStatus,
  onChatRooms,
  onChatTimeline,
  onChatReceipts,
  onChatTyping,
  onChatVerification,
  onChatMediaProgress,
  onChatOpen,
  type Unsubscribe,
} from '../lib/events'
import { openSpace } from './space'
import type {
  ChatStatus,
  ChatRoom,
  ChatMessage,
  ChatMember,
  ChatVerification,
  ChatMediaProgress,
  ChatSendRequest,
} from '../lib/types'

/** The account before anything is known, and while chat is off. */
export const signedOutStatus: ChatStatus = {
  state: 'signedOut',
  userId: '',
  deviceId: '',
  homeserver: '',
  displayName: '',
  avatar: '',
  error: '',
  encryption: {
    ready: false,
    deviceVerified: false,
    crossSigning: false,
    secretStorage: false,
    secretStoragePassphrase: false,
    backupVersion: '',
    backupKeys: 0,
    backupActive: false,
    fingerprint: '',
  },
}

/** The chat account's state. */
export const chatStatus = writable<ChatStatus>(signedOutStatus)
/** Every room, direct chat, space and invite, most recent first. */
export const chatRooms = writable<ChatRoom[]>([])
/** The room on screen, or none. */
export const activeRoomId = writable<string | null>(null)

/** One room's loaded timeline. */
export interface RoomTimeline {
  messages: ChatMessage[]
  hasMore: boolean
  fullyRead: string
  loading: boolean
  loadingOlder: boolean
  error: string
}

/** The timelines of rooms opened so far, by room id. */
export const timelines = writable<Record<string, RoomTimeline>>({})

/** A message on its way out, shown until the server echoes it back. */
export interface PendingMessage {
  localId: string
  roomId: string
  text: string
  replyTo: string
  /** A file being sent, by name and size. */
  file: { name: string; size: number } | null
  state: 'sending' | 'failed'
  error: string
  createdAt: number
  /** The event id once the server took it. */
  eventId: string
  request: ChatSendRequest | null
}

/** Messages on their way out, by room id. */
export const pending = writable<Record<string, PendingMessage[]>>({})
/** Who is typing, by room id. */
export const typing = writable<Record<string, ChatMember[]>>({})
/** Device verifications in flight. */
export const verifications = writable<ChatVerification[]>([])
/** Files being saved or sent, by event id or upload id. */
export const mediaProgress = writable<Record<string, ChatMediaProgress>>({})

/** True while the account is signed in, whatever the connection. */
export const chatSignedIn = derived(chatStatus, ($s) => $s.state !== 'signedOut' && $s.state !== 'stopped')

/** How many rooms have something unread, for the space's badge. */
export const chatUnreadRooms = derived(chatRooms, ($rooms) =>
  $rooms.filter((r) => r.membership === 'invite' || r.unread > 0).length,
)

/** True when a room with a mention or a direct message is unread. */
export const chatHasHighlight = derived(chatRooms, ($rooms) =>
  $rooms.some((r) => r.membership === 'invite' || r.highlight > 0 || (r.kind === 'direct' && r.unread > 0)),
)

/** How many messages a timeline loads at a time. */
export const pageSize = 40

/** The most messages a reload of an open timeline fetches. */
const reloadCap = 300

let unsubs: Unsubscribe[] = []
let started = false
let roomsTimer: ReturnType<typeof setTimeout> | null = null
const reloadTimers = new Map<string, ReturnType<typeof setTimeout>>()

/** chatError turns a backend error into a sentence for a toast or a form. */
export function chatError(err: unknown): string {
  let text = err instanceof Error ? err.message : typeof err === 'string' ? err : ''
  text = text.replace(/^(pelton|chat): /, '')
  return text ? text.charAt(0).toUpperCase() + text.slice(1) : 'Something went wrong'
}

/**
 * Follows the backend's chat events and loads the account and rooms. Called
 * when chat is turned on; does nothing when already running.
 */
export async function startChat(): Promise<void> {
  if (started) {
    return
  }
  started = true
  unsubs = [
    onChatStatus((s) => {
      const was = get(chatStatus).state
      chatStatus.set(s)
      if (was === 'signedOut' && s.state !== 'signedOut') {
        scheduleRooms()
      }
      if (s.state === 'signedOut') {
        resetData()
      }
    }),
    onChatRooms(() => scheduleRooms()),
    onChatTimeline((roomId) => {
      if (roomId) {
        scheduleReload(roomId)
      } else {
        for (const id of Object.keys(get(timelines))) {
          scheduleReload(id)
        }
      }
    }),
    onChatReceipts((roomId) => scheduleReload(roomId)),
    onChatTyping((roomId, users) => typing.update((all) => ({ ...all, [roomId]: users }))),
    onChatVerification((v) => upsertVerification(v)),
    onChatMediaProgress((p) => {
      mediaProgress.update((all) => {
        const next = { ...all }
        if (p.running) {
          next[p.key] = p
        } else {
          delete next[p.key]
        }
        return next
      })
    }),
    onChatOpen((roomId) => {
      openSpace('chat')
      void openRoom(roomId)
    }),
  ]
  await refreshStatus()
  if (get(chatStatus).state !== 'signedOut') {
    await Promise.all([refreshRooms(), refreshVerifications()])
  }
}

/** Stops following chat events and forgets everything loaded. */
export function stopChat(): void {
  for (const u of unsubs) {
    u()
  }
  unsubs = []
  started = false
  chatStatus.set(signedOutStatus)
  resetData()
}

function resetData(): void {
  chatRooms.set([])
  activeRoomId.set(null)
  timelines.set({})
  pending.set({})
  typing.set({})
  verifications.set([])
  mediaProgress.set({})
}

/** Reads the account's state again. */
export async function refreshStatus(): Promise<void> {
  try {
    chatStatus.set(await fetchStatus())
  } catch {
    chatStatus.set(signedOutStatus)
  }
}

/** Reads the room list again. */
export async function refreshRooms(): Promise<void> {
  try {
    chatRooms.set(await fetchRooms())
  } catch {
    // signed out between the event and the read
  }
}

async function refreshVerifications(): Promise<void> {
  try {
    verifications.set(await chatVerifications())
  } catch {
    verifications.set([])
  }
}

function scheduleRooms(): void {
  if (roomsTimer) {
    clearTimeout(roomsTimer)
  }
  roomsTimer = setTimeout(() => {
    roomsTimer = null
    void refreshRooms()
  }, 120)
}

function scheduleReload(roomId: string): void {
  if (!get(timelines)[roomId]) {
    return
  }
  const t = reloadTimers.get(roomId)
  if (t) {
    clearTimeout(t)
  }
  reloadTimers.set(
    roomId,
    setTimeout(() => {
      reloadTimers.delete(roomId)
      void reloadTimeline(roomId)
    }, 80),
  )
}

/** Adds or replaces one verification. */
export function upsertVerification(v: ChatVerification): void {
  verifications.update((list) => {
    const i = list.findIndex((x) => x.id === v.id)
    if (i < 0) {
      return [...list, v]
    }
    const next = list.slice()
    next[i] = v
    return next
  })
}

/** Drops a finished verification from the list. */
export function forgetVerification(id: string): void {
  verifications.update((list) => list.filter((v) => v.id !== id))
}

function setTimeline(roomId: string, patch: Partial<RoomTimeline>): void {
  timelines.update((all) => {
    const cur: RoomTimeline = all[roomId] ?? { messages: [], hasMore: true, fullyRead: '', loading: false, loadingOlder: false, error: '' }
    return { ...all, [roomId]: { ...cur, ...patch } }
  })
}

/** Shows a room and loads its newest messages the first time. */
export async function openRoom(roomId: string): Promise<void> {
  activeRoomId.set(roomId)
  void chatSetViewing(roomId)
  void chatTypists(roomId)
    .then((users) => typing.update((all) => ({ ...all, [roomId]: users })))
    .catch(() => {})
  if (!get(timelines)[roomId]) {
    await reloadTimeline(roomId)
  }
}

/** Leaves the room on screen. */
export function closeRoom(): void {
  activeRoomId.set(null)
  void chatSetViewing('')
}

/**
 * Loads the newest messages of a room again, as many as are already shown so
 * the reader keeps their place, and settles messages the server echoed back.
 */
export async function reloadTimeline(roomId: string): Promise<void> {
  const cur = get(timelines)[roomId]
  const limit = Math.min(Math.max(pageSize, cur?.messages.length ?? 0), reloadCap)
  setTimeline(roomId, { loading: !cur, error: '' })
  try {
    const page = await chatTimeline(roomId, '', limit)
    const keepOlder = cur && cur.messages.length > reloadCap
    setTimeline(roomId, {
      messages: page.messages ?? [],
      hasMore: keepOlder ? true : page.hasMore,
      fullyRead: page.fullyRead,
      loading: false,
    })
    settlePending(roomId, page.messages ?? [])
  } catch (err) {
    setTimeline(roomId, { loading: false, error: chatError(err) })
  }
}

/** Loads one more page of older messages. */
export async function loadOlder(roomId: string): Promise<void> {
  const cur = get(timelines)[roomId]
  if (!cur || cur.loadingOlder || !cur.hasMore) {
    return
  }
  const first = cur.messages[0]
  setTimeline(roomId, { loadingOlder: true })
  try {
    const page = await chatTimeline(roomId, first?.id ?? '', pageSize)
    const now = get(timelines)[roomId] ?? cur
    const seen = new Set(now.messages.map((m) => m.id))
    const older = (page.messages ?? []).filter((m) => !seen.has(m.id))
    setTimeline(roomId, { messages: [...older, ...now.messages], hasMore: page.hasMore, loadingOlder: false })
  } catch (err) {
    setTimeline(roomId, { loadingOlder: false, error: chatError(err) })
  }
}

/** Drops pending messages whose event the timeline now holds. */
function settlePending(roomId: string, messages: ChatMessage[]): void {
  const ids = new Set(messages.map((m) => m.id))
  pending.update((all) => {
    const list = all[roomId]
    if (!list?.some((p) => p.eventId && ids.has(p.eventId))) {
      return all
    }
    return { ...all, [roomId]: list.filter((p) => !(p.eventId && ids.has(p.eventId))) }
  })
}

let localSeq = 0

function newLocalId(): string {
  localSeq += 1
  return `local-${Date.now().toString(36)}-${localSeq}`
}

function putPending(p: PendingMessage): void {
  pending.update((all) => ({ ...all, [p.roomId]: [...(all[p.roomId] ?? []), p] }))
}

function patchPending(roomId: string, localId: string, patch: Partial<PendingMessage>): void {
  pending.update((all) => ({
    ...all,
    [roomId]: (all[roomId] ?? []).map((p) => (p.localId === localId ? { ...p, ...patch } : p)),
  }))
}

/** Removes a pending message, such as a failed one the user gave up on. */
export function discardPending(roomId: string, localId: string): void {
  pending.update((all) => ({ ...all, [roomId]: (all[roomId] ?? []).filter((p) => p.localId !== localId) }))
}

/**
 * Sends a message. A new message shows at once as pending and settles when
 * the server echoes it; an edit only changes the message once it is back.
 */
export async function sendMessage(req: ChatSendRequest): Promise<void> {
  if (req.edit) {
    await chatSend(req)
    return
  }
  const p: PendingMessage = {
    localId: newLocalId(),
    roomId: req.roomId,
    text: req.text,
    replyTo: req.replyTo,
    file: null,
    state: 'sending',
    error: '',
    createdAt: Date.now(),
    eventId: '',
    request: req,
  }
  putPending(p)
  await deliver(p)
}

async function deliver(p: PendingMessage): Promise<void> {
  if (!p.request) {
    return
  }
  try {
    const eventId = await chatSend(p.request)
    patchPending(p.roomId, p.localId, { eventId })
    const tl = get(timelines)[p.roomId]
    if (tl) {
      settlePending(p.roomId, tl.messages)
    }
  } catch (err) {
    patchPending(p.roomId, p.localId, { state: 'failed', error: chatError(err) })
  }
}

/** Tries a failed message again. */
export async function retryPending(roomId: string, localId: string): Promise<void> {
  const p = (get(pending)[roomId] ?? []).find((x) => x.localId === localId)
  if (!p || !p.request) {
    return
  }
  patchPending(roomId, localId, { state: 'sending', error: '' })
  await deliver({ ...p, state: 'sending' })
}

/** Sends files from disk; each shows as pending with its upload progress. */
export async function sendFilePaths(roomId: string, paths: string[], replyTo: string): Promise<void> {
  const items = paths.map((path) => {
    const name = path.split(/[\\/]/).pop() || path
    const p: PendingMessage = {
      localId: newLocalId(),
      roomId,
      text: '',
      replyTo,
      file: { name, size: 0 },
      state: 'sending',
      error: '',
      createdAt: Date.now(),
      eventId: '',
      request: null,
    }
    putPending(p)
    return p
  })
  try {
    await chatSendFilePaths(roomId, paths, items.map((p) => p.localId), replyTo)
    for (const p of items) {
      discardPending(roomId, p.localId)
    }
  } catch (err) {
    for (const p of items) {
      patchPending(roomId, p.localId, { state: 'failed', error: chatError(err) })
    }
  }
}

/** Sends a file the frontend holds, such as a pasted image. */
export async function sendFileBlob(roomId: string, file: File, replyTo: string): Promise<void> {
  const p: PendingMessage = {
    localId: newLocalId(),
    roomId,
    text: '',
    replyTo,
    file: { name: file.name || 'image', size: file.size },
    state: 'sending',
    error: '',
    createdAt: Date.now(),
    eventId: '',
    request: null,
  }
  putPending(p)
  try {
    const data = await blobToBase64(file)
    const eventId = await chatSendFileData(roomId, file.name || 'image', file.type, data, p.localId, replyTo)
    patchPending(roomId, p.localId, { eventId })
    const tl = get(timelines)[roomId]
    if (tl) {
      settlePending(roomId, tl.messages)
    }
  } catch (err) {
    patchPending(roomId, p.localId, { state: 'failed', error: chatError(err) })
  }
}

/** Reads a blob as base64, without the data url prefix. */
export function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const s = String(reader.result ?? '')
      resolve(s.slice(s.indexOf(',') + 1))
    }
    reader.onerror = () => reject(reader.error ?? new Error('read failed'))
    reader.readAsDataURL(blob)
  })
}

/** Adds the user's reaction with a key, or takes it back when already given. */
export async function toggleReaction(roomId: string, message: ChatMessage, key: string): Promise<void> {
  const mine = message.reactions.find((r) => r.key === key)?.mine
  if (mine) {
    await chatRedact(roomId, mine)
  } else {
    await chatReact(roomId, message.id, key)
  }
}

/** Removes a message for everyone. */
export async function redactMessage(roomId: string, eventId: string): Promise<void> {
  await chatRedact(roomId, eventId)
}

/**
 * Moves the read marker to the newest message when the room has anything
 * unread or the marker is behind. Pending messages do not count.
 */
export async function markRoomRead(roomId: string): Promise<void> {
  const tl = get(timelines)[roomId]
  const last = tl?.messages.at(-1)
  if (!last) {
    return
  }
  const room = get(chatRooms).find((r) => r.id === roomId)
  if (room && room.unread === 0 && room.highlight === 0 && tl.fullyRead === last.id) {
    return
  }
  setTimeline(roomId, { fullyRead: last.id })
  chatRooms.update((rooms) => rooms.map((r) => (r.id === roomId ? { ...r, unread: 0, highlight: 0, fullyRead: last.id } : r)))
  try {
    await chatMarkRead(roomId, last.id)
  } catch {
    // the next sync brings the server's counts back
  }
}
