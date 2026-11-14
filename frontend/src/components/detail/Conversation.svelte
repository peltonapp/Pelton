<script lang="ts">
  // the reading pane of the new design: the open message's conversation as a
  // stack of cards, oldest first. The newest message, the one opened from the
  // list and anything unread start open; the rest are one line each and open
  // on a click. A long run of closed older messages folds into one line too.
  // Below the cards sits the quick reply, and a floating action bar stays at
  // the bottom of the pane. With the list not grouped the conversation is the
  // one message.
  import { createEventDispatcher, tick } from 'svelte'
  import narrowIcon from '@phosphor-icons/core/assets/duotone/arrows-in-line-horizontal-duotone.svg?raw'
  import mediumIcon from '@phosphor-icons/core/assets/duotone/text-align-justify-duotone.svg?raw'
  import fullIcon from '@phosphor-icons/core/assets/duotone/arrows-out-line-horizontal-duotone.svg?raw'
  import smallerIcon from '@phosphor-icons/core/assets/duotone/minus-duotone.svg?raw'
  import largerIcon from '@phosphor-icons/core/assets/duotone/plus-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import ConversationCard from './ConversationCard.svelte'
  import QuickReply from './QuickReply.svelte'
  import ReaderActionBar from './ReaderActionBar.svelte'
  import { prefs, setMessageFontSize, setReaderWidth } from '../../stores/prefs'
  import { threadedOn, threadMessages, cardDetails, loadThread, loadCard } from '../../stores/threads'
  import { patchInList } from '../../stores/messages'
  import { setSeen } from '../../lib/api'
  import { sameMessage } from '../../lib/threadrows'
  import { errorMessage, toastError } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { MessageDetail, MessageSummary, ReaderWidth, ThreadMessage } from '../../lib/types'

  /** The message opened from the list, whose conversation is shown. */
  export let detail: MessageDetail

  /** What a reader action is asked to do, and to which message. */
  type Action = 'reply' | 'replyAll' | 'forward' | 'archive' | 'delete' | 'reload'

  const dispatch = createEventDispatcher<{
    action: { kind: Action; message: MessageDetail }
    more: { message: MessageDetail; x: number; y: number }
  }>()

  // the message type sizes the reading pane offers, smallest to largest.
  const fontSizes = [12, 14, 16, 18, 20]

  $: threadId = threadedOn($prefs) ? detail.threadId : 0
  let threadError = ''
  $: void fetchThread(threadId)

  async function fetchThread(id: number): Promise<void> {
    threadError = ''
    if (id === 0) {
      return
    }
    try {
      await loadThread(id)
    } catch (err) {
      threadError = errorMessage(err)
    }
  }

  $: messages = conversationOf(threadId === 0 ? undefined : $threadMessages.get(threadId), detail)

  // conversationOf is the cards to show: the thread, or the opened message on
  // its own until the thread has loaded. A thread that left the opened message
  // out (it sits in Trash) still shows it, in date order.
  function conversationOf(members: ThreadMessage[] | undefined, opened: MessageDetail): ThreadMessage[] {
    const own: ThreadMessage = { ...opened, folderRole: '', fromMe: false }
    if (!members || members.length === 0) {
      return [own]
    }
    if (members.some((m) => sameMessage(m, opened))) {
      return members
    }
    return [...members, own].sort((a, b) => (a.date < b.date ? -1 : a.date > b.date ? 1 : 0))
  }

  // which cards are open, by message id. A card that appears for the first
  // time opens when it is the newest, the opened one or unread; after that
  // only the reader opens and closes them.
  let openIds = new Set<number>()
  let known = new Set<number>()
  let conversationKey = ''
  // the card the action bar acts on: the opened message, or the last card the
  // reader opened or answered from.
  let focusId = 0
  // the folded run of closed older messages has been shown.
  let revealed = false

  $: syncOpen(messages, `${threadId}:${detail.id}`)

  function syncOpen(list: ThreadMessage[], key: string): void {
    if (key !== conversationKey) {
      conversationKey = key
      openIds = new Set()
      known = new Set()
      revealed = false
      focusId = detail.id
      void scrollToFocus()
    }
    const newest = list[list.length - 1]
    let changed = false
    const next = new Set(openIds)
    for (const m of list) {
      if (known.has(m.id)) {
        continue
      }
      known.add(m.id)
      if (m.id === newest.id || !m.seen || sameMessage(m, detail)) {
        next.add(m.id)
        changed = true
      }
    }
    if (changed) {
      openIds = next
    }
  }

  // the full messages of the open cards other than the opened one, which the
  // pane already holds.
  let cardErrors = new Map<number, string>()
  const fetching = new Set<number>()
  $: for (const m of messages) {
    if (openIds.has(m.id) && !sameMessage(m, detail) && !$cardDetails.has(m.id) && !fetching.has(m.id)) {
      void fetchCard(m.id)
    }
  }

  async function fetchCard(id: number): Promise<void> {
    fetching.add(id)
    try {
      await loadCard(id)
    } catch (err) {
      cardErrors = new Map(cardErrors).set(id, errorMessage(err))
    } finally {
      fetching.delete(id)
    }
  }

  function detailOf(m: MessageSummary, cards: Map<number, MessageDetail>): MessageDetail | null {
    return sameMessage(m, detail) ? detail : (cards.get(m.id) ?? null)
  }

  // opening a card reads the message, the way opening it from the list does.
  // The opened message itself was marked by the list.
  const marked = new Set<number>()
  $: if ($prefs.markReadOnOpen) {
    for (const m of messages) {
      if (openIds.has(m.id) && !m.seen && !sameMessage(m, detail) && !marked.has(m.id)) {
        marked.add(m.id)
        void markRead(m.id)
      }
    }
  }

  async function markRead(id: number): Promise<void> {
    patchInList(id, { seen: true })
    try {
      await setSeen(id, true)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function toggle(m: ThreadMessage): void {
    const next = new Set(openIds)
    if (next.has(m.id)) {
      next.delete(m.id)
    } else {
      next.add(m.id)
      focusId = m.id
    }
    openIds = next
  }

  // the closed older messages between the first and the last two fold into
  // one line once there are enough of them to be worth it.
  $: folded = revealed ? [] : foldedRun(messages, openIds)

  function foldedRun(list: ThreadMessage[], open: Set<number>): number[] {
    const run: number[] = []
    for (let i = 1; i < list.length - 2; i++) {
      if (open.has(list[i].id)) {
        break
      }
      run.push(list[i].id)
    }
    return run.length >= 2 ? run : []
  }

  $: focusMessage =
    messages.find((m) => m.id === focusId) ??
    (focusId === detail.id ? messages.find((m) => sameMessage(m, detail)) : undefined) ??
    messages[messages.length - 1]
  $: focusDetail = detailOf(focusMessage, $cardDetails) ?? detail
  $: newest = messages[messages.length - 1]
  $: newestDetail = detailOf(newest, $cardDetails)
  $: subject = messages[0]?.subject || detail.subject || $t('detail.noSubject')

  function act(kind: Action, message: MessageDetail | null): void {
    if (message) {
      focusId = message.id
      dispatch('action', { kind, message })
    }
  }

  function more(message: MessageDetail | null, at: { x: number; y: number }): void {
    if (message) {
      dispatch('more', { message, ...at })
    }
  }

  $: fontIndex = Math.max(0, fontSizes.indexOf($prefs.messageFontSize))

  function stepFont(by: number): void {
    const next = fontSizes[Math.min(fontSizes.length - 1, Math.max(0, fontIndex + by))]
    if (next !== $prefs.messageFontSize) {
      setMessageFontSize(next)
    }
  }

  $: widths = [
    { key: 'narrow' as ReaderWidth, icon: narrowIcon, label: $t('reader.width.narrow') },
    { key: 'medium' as ReaderWidth, icon: mediumIcon, label: $t('reader.width.medium') },
    { key: 'full' as ReaderWidth, icon: fullIcon, label: $t('reader.width.full') },
  ]

  let scrollEl: HTMLDivElement | undefined
  let cardEls: Record<number, HTMLElement> = {}

  // a new conversation starts at its top, unless the opened message sits
  // further down, in which case it is brought into view.
  async function scrollToFocus(): Promise<void> {
    await tick()
    if (!scrollEl) {
      return
    }
    scrollEl.scrollTop = 0
    const el = cardEls[focusId]
    if (el && messages[0]?.id !== focusId) {
      scrollEl.scrollTop = Math.max(0, el.offsetTop - scrollEl.offsetTop - scrollEl.clientHeight / 4)
    }
  }
</script>

<div class="scroll selectable" tabindex="-1" bind:this={scrollEl}>
  <div class="column" data-width={$prefs.readerWidth}>
    <header class="conv-head">
      <div class="title">
        <h1 dir="auto">{subject}</h1>
        {#if messages.length > 1}
          <span class="count">{$t('reader.count').replace('{count}', String(messages.length))}</span>
        {/if}
      </div>
      <div class="view" role="group" aria-label={$t('reader.view')}>
        <button
          type="button"
          class="tool"
          aria-label={$t('reader.text.smaller')}
          title={$t('reader.text.smaller')}
          disabled={fontIndex === 0}
          on:click={() => stepFont(-1)}
        >
          <Ph svg={smallerIcon} size={14} />
        </button>
        <span class="aa" aria-hidden="true">Aa</span>
        <button
          type="button"
          class="tool"
          aria-label={$t('reader.text.larger')}
          title={$t('reader.text.larger')}
          disabled={fontIndex === fontSizes.length - 1}
          on:click={() => stepFont(1)}
        >
          <Ph svg={largerIcon} size={14} />
        </button>
        <span class="sep" aria-hidden="true"></span>
        <div class="widths" role="radiogroup" aria-label={$t('reader.width')}>
          {#each widths as w (w.key)}
            <button
              type="button"
              class="tool"
              role="radio"
              aria-checked={$prefs.readerWidth === w.key}
              aria-label={w.label}
              title={w.label}
              on:click={() => setReaderWidth(w.key)}
            >
              <Ph svg={w.icon} size={14} />
            </button>
          {/each}
        </div>
      </div>
    </header>

    {#if threadError}
      <p class="thread-error">{threadError}</p>
    {/if}

    <div class="cards">
      {#each messages as m (m.id)}
        {#if folded.length > 0 && m.id === folded[0]}
          <button type="button" class="more-run" on:click={() => (revealed = true)}>
            <span class="rule" aria-hidden="true"></span>
            <span class="more-label">{$t('reader.moreMessages').replace('{count}', String(folded.length))}</span>
            <span class="rule" aria-hidden="true"></span>
          </button>
        {/if}
        {#if !folded.includes(m.id)}
          <div class="slot" bind:this={cardEls[m.id]}>
            <ConversationCard
              message={m}
              detail={detailOf(m, $cardDetails)}
              open={openIds.has(m.id)}
              closable={messages.length > 1}
              fromMe={m.fromMe}
              error={cardErrors.get(m.id) ?? ''}
              on:toggle={() => toggle(m)}
              on:reply={() => act('reply', detailOf(m, $cardDetails))}
              on:more={(e) => more(detailOf(m, $cardDetails), e.detail)}
              on:reload={() => act('reload', detailOf(m, $cardDetails))}
            />
          </div>
        {/if}
      {/each}
    </div>

    <QuickReply detail={newestDetail} />
  </div>

  <ReaderActionBar
    on:reply={() => act('reply', focusDetail)}
    on:replyAll={() => act('replyAll', focusDetail)}
    on:forward={() => act('forward', focusDetail)}
    on:archive={() => act('archive', focusDetail)}
    on:delete={() => act('delete', focusDetail)}
    on:more={(e) => more(focusDetail, e.detail)}
  />
</div>

<style>
  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    background: var(--surface-base);
  }

  .scroll:focus {
    outline: none;
  }

  .column {
    width: 100%;
    margin-inline: auto;
    padding: var(--pane-pad) var(--pane-pad) calc(var(--control-height) * 2 + var(--space-6));
  }

  .column[data-width='narrow'] {
    max-width: var(--reader-width-narrow);
  }

  .column[data-width='medium'] {
    max-width: var(--reader-width-medium);
  }

  .conv-head {
    display: flex;
    align-items: flex-start;
    gap: var(--space-4);
    margin-bottom: var(--space-4);
  }

  .title {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  h1 {
    margin: 0;
    font-size: var(--fz-title);
    font-weight: var(--fw-semibold);
    line-height: 1.3;
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  .count {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .view {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    flex-shrink: 0;
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
  }

  .widths {
    display: inline-flex;
    gap: var(--space-1);
  }

  .tool {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .tool:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .tool:disabled {
    cursor: default;
    opacity: 0.4;
  }

  .tool[aria-checked='true'] {
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .aa {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .sep {
    width: var(--hairline);
    align-self: stretch;
    margin-inline: var(--space-1);
    background: var(--border-default);
  }

  .thread-error {
    margin: 0 0 var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .cards {
    display: flex;
    flex-direction: column;
  }

  /* closed cards stack into one block, sharing their hairlines. */
  .slot + .slot {
    margin-top: calc(var(--hairline) * -1);
  }

  .more-run {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    width: 100%;
    padding: var(--space-2) 0;
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .more-run:hover {
    color: var(--text-primary);
  }

  .more-run .rule {
    flex: 1;
    border-top: var(--hairline) solid var(--border-default);
  }

  .more-label {
    padding: var(--space-1) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
  }
</style>
