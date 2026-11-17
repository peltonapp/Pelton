<script lang="ts">
  // a room's timeline: day separators, the unread marker where the user left
  // off, grouped messages and the ones still on their way out. Scrolling to
  // the top loads older messages without the view jumping; new messages keep
  // the view at the bottom while it is there, and offer a way back to the
  // latest when it is not. The arrow keys move a cursor through the messages
  // and r, e and Delete act on the one under it.
  import { afterUpdate, beforeUpdate, createEventDispatcher, onDestroy, tick } from 'svelte'
  import arrowDownIcon from '@phosphor-icons/core/assets/duotone/arrow-down-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import MessageRow from './MessageRow.svelte'
  import {
    timelines,
    pending,
    chatStatus,
    loadOlder,
    markRoomRead,
    toggleReaction,
    redactMessage,
    retryPending,
    discardPending,
    chatError,
  } from '../../stores/chat'
  import { chatRetryDecryption } from '../../lib/api'
  import { buildTimeline } from '../../lib/chattimeline'
  import { askConfirm } from '../../stores/confirm'
  import { toastError } from '../../stores/toast'
  import { formatListDate, type TimeFormat } from '../../lib/format'
  import { prefs } from '../../stores/prefs'
  import { t } from '../../lib/i18n'
  import type { ChatMessage, ChatRoom } from '../../lib/types'

  /** The room whose timeline this is. */
  export let room: ChatRoom

  const dispatch = createEventDispatcher<{ reply: ChatMessage; edit: ChatMessage }>()

  let scroller: HTMLElement
  let atBottom = true
  let unreadAfter = ''
  let markerRoom = ''
  let cursor = -1
  let keepFromBottom: number | null = null
  let lastCount = 0
  let readTimer: ReturnType<typeof setTimeout> | null = null

  $: tl = $timelines[room.id]
  $: messages = tl?.messages ?? []
  $: queued = $pending[room.id] ?? []
  // where the unread marker goes is decided once per visit, when the room
  // opens; reading moves the stored marker, not the line on screen.
  $: if (tl && !tl.loading && room.id !== markerRoom) {
    markerRoom = room.id
    unreadAfter = room.unread > 0 || room.highlight > 0 ? tl.fullyRead : ''
    cursor = -1
    void tick().then(scrollToMarkerOrBottom)
  }
  $: items = buildTimeline(messages, {
    unreadAfter,
    ownId: $chatStatus.userId,
    pending: queued.map((p) => ({ localId: p.localId, text: p.text, createdAt: p.createdAt })),
  })
  $: messageIds = items.filter((i) => i.type === 'message').map((i) => (i.type === 'message' ? i.key : ''))
  $: newBelow = !atBottom && messages.length > lastCount

  beforeUpdate(() => {
    if (scroller && keepFromBottom === null && tl?.loadingOlder) {
      keepFromBottom = scroller.scrollHeight - scroller.scrollTop
    }
  })

  afterUpdate(() => {
    if (!scroller) {
      return
    }
    if (keepFromBottom !== null && !tl?.loadingOlder) {
      scroller.scrollTop = scroller.scrollHeight - keepFromBottom
      keepFromBottom = null
      return
    }
    if (atBottom) {
      scroller.scrollTop = scroller.scrollHeight
      lastCount = messages.length
      scheduleRead()
    }
  })

  onDestroy(() => {
    if (readTimer) {
      clearTimeout(readTimer)
    }
  })

  function scrollToMarkerOrBottom(): void {
    if (!scroller) {
      return
    }
    const marker = scroller.querySelector<HTMLElement>('[data-unread-marker]')
    if (marker) {
      marker.scrollIntoView({ block: 'center' })
      onScroll()
    } else {
      scroller.scrollTop = scroller.scrollHeight
      atBottom = true
      scheduleRead()
    }
  }

  function onScroll(): void {
    if (!scroller) {
      return
    }
    const gap = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight
    atBottom = gap < 40
    if (atBottom) {
      lastCount = messages.length
      scheduleRead()
    }
    if (scroller.scrollTop < 200 && tl?.hasMore && !tl.loadingOlder) {
      void loadOlder(room.id)
    }
  }

  // reading only counts while the window has focus and the newest message is
  // on screen.
  function scheduleRead(): void {
    if (readTimer) {
      clearTimeout(readTimer)
    }
    readTimer = setTimeout(() => {
      readTimer = null
      if (atBottom && document.hasFocus()) {
        void markRoomRead(room.id)
      }
    }, 600)
  }

  function jumpToLatest(): void {
    scroller.scrollTo({ top: scroller.scrollHeight, behavior: $prefs.reduceMotion ? 'auto' : 'smooth' })
  }

  function jumpTo(id: string): void {
    const el = scroller?.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(id)}"]`)
    if (el) {
      el.scrollIntoView({ block: 'center', behavior: $prefs.reduceMotion ? 'auto' : 'smooth' })
      cursor = messageIds.indexOf(id)
    }
  }

  async function react(message: ChatMessage, key: string): Promise<void> {
    try {
      await toggleReaction(room.id, message, key)
    } catch (err) {
      toastError(chatError(err))
    }
  }

  async function redact(message: ChatMessage): Promise<void> {
    const ok = await askConfirm({
      title: $t('chat.message.deleteTitle'),
      body: $t('chat.message.deleteBody'),
      confirmLabel: $t('chat.message.delete'),
      danger: true,
    })
    if (!ok) {
      return
    }
    try {
      await redactMessage(room.id, message.id)
    } catch (err) {
      toastError(chatError(err))
    }
  }

  async function retry(message: ChatMessage): Promise<void> {
    const ids = messages.filter((m) => m.kind === 'encrypted' && m.missingKeys).map((m) => m.id)
    try {
      await chatRetryDecryption(room.id, ids.length ? ids : [message.id])
    } catch (err) {
      toastError(chatError(err))
    }
  }

  function selectedMessage(): ChatMessage | undefined {
    const id = messageIds[cursor]
    return messages.find((m) => m.id === id)
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.target !== scroller || event.metaKey || event.ctrlKey || event.altKey) {
      return
    }
    if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
      event.preventDefault()
      const step = event.key === 'ArrowUp' ? -1 : 1
      cursor = cursor < 0 ? messageIds.length - 1 : Math.max(0, Math.min(messageIds.length - 1, cursor + step))
      void tick().then(() => {
        scroller.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(messageIds[cursor] ?? '')}"]`)?.scrollIntoView({ block: 'nearest' })
      })
      return
    }
    const msg = selectedMessage()
    if (!msg) {
      return
    }
    if (event.key === 'r') {
      event.preventDefault()
      dispatch('reply', msg)
    } else if (event.key === 'e' && msg.own) {
      event.preventDefault()
      dispatch('edit', msg)
    } else if ((event.key === 'Delete' || event.key === 'Backspace') && msg.own) {
      event.preventDefault()
      void redact(msg)
    } else if (event.key === 'Escape') {
      cursor = -1
    }
  }

  function dayLabel(ts: number): string {
    const d = new Date(ts)
    const today = new Date()
    const yesterday = new Date(today.getFullYear(), today.getMonth(), today.getDate() - 1)
    if (d.toDateString() === today.toDateString()) {
      return $t('chat.day.today')
    }
    if (d.toDateString() === yesterday.toDateString()) {
      return $t('chat.day.yesterday')
    }
    return d.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long', year: d.getFullYear() === today.getFullYear() ? undefined : 'numeric' })
  }
</script>

<div class="wrap">
  <!-- svelte-ignore a11y-no-noninteractive-tabindex a11y_no_noninteractive_element_interactions -->
  <div
    class="timeline"
    bind:this={scroller}
    on:scroll={onScroll}
    on:keydown={onKeydown}
    tabindex="0"
    role="log"
    aria-label={$t('chat.timeline.label')}
    aria-live="polite"
  >
    {#if tl?.loadingOlder}
      <div class="older"><PieProgress label={$t('chat.timeline.loadingOlder')} /></div>
    {:else if tl && !tl.hasMore && messages.length > 0}
      <div class="older start">{$t('chat.timeline.start')}</div>
    {/if}
    {#if !tl || tl.loading}
      <div class="older"><PieProgress size={18} label={$t('chat.space.loading')} /></div>
    {:else if tl.error && messages.length === 0}
      <p class="error">{tl.error}</p>
    {/if}

    {#each items as item (item.key)}
      {#if item.type === 'day'}
        <div class="day" role="separator"><span>{dayLabel(item.ts)}</span></div>
      {:else if item.type === 'unread'}
        <div class="unread" data-unread-marker role="separator"><span>{$t('chat.timeline.new')}</span></div>
      {:else if item.type === 'message'}
        <MessageRow
          message={item.message}
          roomId={room.id}
          continued={item.continued}
          selected={messageIds[cursor] === item.key}
          on:reply={(e) => dispatch('reply', e.detail)}
          on:edit={(e) => dispatch('edit', e.detail)}
          on:redact={(e) => void redact(e.detail)}
          on:react={(e) => void react(e.detail.message, e.detail.key)}
          on:retry={(e) => void retry(e.detail)}
          on:jump={(e) => jumpTo(e.detail)}
        />
      {:else}
        {@const p = queued.find((x) => x.localId === item.localId)}
        {#if p}
          <div class="pending" class:failed={p.state === 'failed'} class:continued={item.continued}>
            <span class="pending-body">{p.file ? p.file.name : p.text}</span>
            <span class="pending-state">
              {#if p.state === 'sending'}
                <PieProgress label={$t('chat.pending.sending')} />
                {$t('chat.pending.sending')}
              {:else}
                {p.error || $t('chat.pending.failed')}
                {#if p.request}
                  <button type="button" class="link" on:click={() => void retryPending(room.id, p.localId)}>{$t('chat.pending.retry')}</button>
                {/if}
                <button type="button" class="link" on:click={() => discardPending(room.id, p.localId)}>{$t('chat.pending.discard')}</button>
              {/if}
            </span>
          </div>
        {/if}
      {/if}
    {/each}
  </div>

  {#if newBelow || (!atBottom && messages.length > 0)}
    <button type="button" class="latest" on:click={jumpToLatest}>
      <Ph svg={arrowDownIcon} size={14} />
      {newBelow ? $t('chat.timeline.newBelow') : $t('chat.timeline.latest')}
    </button>
  {/if}
  {#if room.lastTs && messages.length === 0 && tl && !tl.loading && !tl.hasMore}
    <div class="empty">
      <EmptyState
        size="compact"
        title={$t('chat.timeline.emptyTitle')}
        detail={$t('chat.timeline.emptyDetail').replace('{date}', formatListDate(new Date(room.lastTs).toISOString(), $prefs.timeFormat as TimeFormat))}
      />
    </div>
  {/if}
</div>

<style>
  .wrap {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .timeline {
    flex: 1;
    overflow-y: auto;
    padding: var(--space-3) 0 var(--space-2);
    outline: none;
  }
  .timeline:focus-visible {
    box-shadow: inset 0 0 0 var(--hairline) var(--accent);
  }

  .older {
    display: flex;
    justify-content: center;
    padding: var(--space-3);
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .error,
  .empty {
    display: flex;
    justify-content: center;
    margin: var(--space-4) var(--pane-pad);
  }

  .day,
  .unread {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin: var(--space-3) var(--pane-pad) var(--space-2);
    color: var(--text-tertiary);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
  }
  .day::before,
  .day::after,
  .unread::before,
  .unread::after {
    content: '';
    flex: 1;
    border-top: var(--hairline) solid var(--border-subtle);
  }
  .unread {
    color: var(--accent);
  }
  .unread::before,
  .unread::after {
    border-color: var(--accent);
  }

  .pending {
    display: flex;
    flex-direction: column;
    padding: var(--space-1) var(--pane-pad) var(--space-1) calc(var(--pane-pad) + var(--chat-avatar) + var(--space-3));
    color: var(--text-secondary);
    font-size: var(--fz-body);
  }
  .pending-body {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .pending-state {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }
  .pending.failed .pending-state {
    color: var(--danger);
  }

  .link {
    padding: 0;
    border: none;
    background: none;
    color: var(--link);
    font: inherit;
    cursor: var(--cursor-action);
  }

  .latest {
    position: absolute;
    right: var(--pane-pad);
    bottom: var(--space-3);
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
  }
</style>
