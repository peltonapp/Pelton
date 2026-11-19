<script lang="ts">
  // one message of the conversation reader. Closed, it is a single line:
  // sender, the start of the text and the date, and a click opens it. Open, it
  // has the compact address line, the body with its quoted original folded,
  // and the attachment strip.
  import { createEventDispatcher } from 'svelte'
  import { slide } from 'svelte/transition'
  import replyIcon from '@phosphor-icons/core/assets/duotone/arrow-bend-up-left-duotone.svg?raw'
  import moreIcon from '@phosphor-icons/core/assets/duotone/dots-three-duotone.svg?raw'
  import clipIcon from '@phosphor-icons/core/assets/duotone/paperclip-duotone.svg?raw'
  import sentIcon from '@phosphor-icons/core/assets/duotone/paper-plane-tilt-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import Avatar from '../common/Avatar.svelte'
  import Spinner from '../common/Spinner.svelte'
  import DetailHeader from './DetailHeader.svelte'
  import MailBody from './MailBody.svelte'
  import PhishingNotice from './PhishingNotice.svelte'
  import ProtectedNotice from './ProtectedNotice.svelte'
  import AttachmentStrip from './AttachmentStrip.svelte'
  import FollowUpNotice from './FollowUpNotice.svelte'
  import PromiseStrip from './PromiseStrip.svelte'
  import { promiseSettings } from '../../stores/promises'
  import { prefs } from '../../stores/prefs'
  import { formatListDate, displayName, type TimeFormat } from '../../lib/format'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { MessageDetail, MessageSummary } from '../../lib/types'
  import { choiceFor, senderReading } from '../../stores/senderreading'
  import { countWords, readingMinutes, visibleText } from '../../lib/readingtime'

  /** The message as listed, which the closed line is drawn from. */
  export let message: MessageSummary
  /** The full message, null until it has loaded. */
  export let detail: MessageDetail | null = null
  /** The card shows the whole message. */
  export let open: boolean = false
  /** The card can be closed again; false for a conversation of one. */
  export let closable: boolean = true
  /** The user sent this message. */
  export let fromMe: boolean = false
  /** The full message could not be loaded. */
  export let error: string = ''
  /** Sets the body for the reading view: roomier lines. */
  export let roomy: boolean = false

  // the text size kept for this sender, or 0 for the global size.
  $: senderSize = choiceFor($senderReading, message.fromAddress).fontSize
  // how long the body takes to read, said only for a long one.
  $: minutes = detail && textful ? readingMinutes(countWords(visibleText(detail))) : 0
  $: readingLabel = minutes > 0 ? $t('reader.readingTime').replace('{n}', String(minutes)) : ''

  const dispatch = createEventDispatcher<{
    toggle: void
    reply: void
    more: { x: number; y: number }
    reload: void
  }>()

  $: sender = fromMe ? $t('thread.me') : displayName(message.fromName, message.fromAddress)

  // a click on the open card's header closes it, unless it landed on one of
  // its controls or ended a text selection.
  function onHeadClick(event: MouseEvent): void {
    if (!closable) {
      return
    }
    const target = event.target as HTMLElement | null
    if (target?.closest('button, a, input')) {
      return
    }
    if ((window.getSelection()?.toString() ?? '') !== '') {
      return
    }
    dispatch('toggle')
  }

  // hasText tells a message with something to read from one with an empty
  // body, which says so rather than showing a blank strip.
  $: textful = detail ? hasText(detail) : false

  function hasText(d: MessageDetail): boolean {
    if (!d.isHtml) {
      return d.bodyPlain.trim() !== ''
    }
    const body = new DOMParser().parseFromString(d.bodyHtmlSafe, 'text/html').body
    return (body.textContent ?? '').trim() !== '' || body.querySelector('img, video, table, hr') !== null
  }

  function onMore(event: MouseEvent): void {
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
    dispatch('more', { x: rect.left, y: rect.bottom })
  }
</script>

<article class="card" class:open class:unread={!message.seen} class:from-me={fromMe} aria-label={sender}>
  {#if open}
    <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
    <div class="head" class:closable on:click={onHeadClick}>
      {#if detail}
        <DetailHeader {detail} compact readingTime={readingLabel}>
          <div slot="actions" class="actions">
            <button
              type="button"
              class="icon"
              aria-label={$t('action.reply')}
              title={$t('action.reply')}
              on:click={() => dispatch('reply')}
            >
              <Ph svg={replyIcon} size={16} />
            </button>
            <button
              type="button"
              class="icon"
              aria-label={$t('reader.more')}
              title={$t('reader.more')}
              on:click={onMore}
            >
              <Ph svg={moreIcon} size={16} />
            </button>
          </div>
        </DetailHeader>
      {:else}
        <div class="line static">
          <Avatar name={message.fromName} email={message.fromAddress} size={18} />
          <span class="name">{sender}</span>
          <time class="date">{formatListDate(message.date, $prefs.timeFormat as TimeFormat)}</time>
        </div>
      {/if}
    </div>
    <div class="body" transition:slide={{ duration: dur(motion.base) }}>
      {#if detail}
        {#if detail.messageId}
          <FollowUpNotice messageId={detail.messageId} />
        {/if}
        {#if detail.phishing.level !== 'none'}
          <PhishingNotice report={detail.phishing} messageId={detail.id} />
        {/if}
        {#if $prefs.design === 'new' && $promiseSettings.enabled && detail.messageId}
          <PromiseStrip messageId={detail.id} accountId={detail.accountId} mailId={detail.messageId} />
        {/if}
        {#if detail.pgpState !== '' && detail.pgpState !== 'open'}
          <ProtectedNotice state={detail.pgpState} messageId={detail.id} on:opened={() => dispatch('reload')} />
        {:else if textful}
          <MailBody {detail} collapseQuotes adaptive fontSize={senderSize} {roomy} />
        {:else}
          <p class="no-text">{$t('messageList.noText')}</p>
        {/if}
        <AttachmentStrip messageId={detail.id} attachments={detail.attachments} />
      {:else if error}
        <p class="error">{error}</p>
      {:else}
        <Spinner label={$t('detail.loadingMessage')} inline />
      {/if}
    </div>
  {:else}
    <button type="button" class="line" aria-expanded="false" on:click={() => dispatch('toggle')}>
      <Avatar name={message.fromName} email={message.fromAddress} size={18} />
      {#if fromMe}
        <span class="sent" title={$t('thread.sent')}><Ph svg={sentIcon} size={12} /></span>
      {/if}
      <span dir="auto" class="name">{sender}</span>
      <span dir="auto" class="snippet" class:none={!message.snippet}>{message.snippet || $t('messageList.noText')}</span>
      {#if message.hasAttachments}
        <span class="clip" aria-label={$t('detail.infoModal.attachments')}><Ph svg={clipIcon} size={12} /></span>
      {/if}
      <time class="date">{formatListDate(message.date, $prefs.timeFormat as TimeFormat)}</time>
    </button>
  {/if}
</article>

<style>
  .card {
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    transition: border-color var(--motion-fast) var(--ease-out);
  }

  .card:not(.open):hover {
    border-color: var(--border-strong);
  }

  .card.open {
    margin-block: var(--space-3);
  }

  .head {
    padding: var(--space-3) var(--space-4) var(--space-2);
  }

  .head.closable {
    cursor: var(--cursor-action);
  }

  .actions {
    display: inline-flex;
    gap: var(--space-1);
    flex-shrink: 0;
  }

  .icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .icon:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-4) var(--space-4);
  }

  .line {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    width: 100%;
    min-width: 0;
    padding: var(--space-3) var(--space-4);
    border: none;
    background: transparent;
    font: inherit;
    text-align: start;
    color: inherit;
    cursor: var(--cursor-action);
  }

  .line.static {
    padding: 0;
    cursor: inherit;
  }

  .name {
    flex-shrink: 0;
    max-width: 40%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-list);
    color: var(--text-secondary);
  }

  .unread .name {
    color: var(--text-primary);
    font-weight: var(--fw-semibold);
  }

  .snippet {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .sent,
  .clip {
    display: inline-flex;
    flex-shrink: 0;
    color: var(--text-tertiary);
  }

  .date {
    flex-shrink: 0;
    margin-inline-start: auto;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .snippet.none {
    font-style: italic;
  }

  .no-text {
    margin: 0;
    font-size: var(--fz-body);
    font-style: italic;
    color: var(--text-tertiary);
  }

  .error {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--danger);
  }
</style>
