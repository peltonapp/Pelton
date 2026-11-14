<script lang="ts">
  // the reply box at the end of a conversation. A short answer is written and
  // sent here without opening the composer; the expand button carries what was
  // typed into the full composer instead. It answers the newest message.
  import expandIcon from '@phosphor-icons/core/assets/duotone/arrows-out-simple-duotone.svg?raw'
  import sendIcon from '@phosphor-icons/core/assets/duotone/paper-plane-tilt-duotone.svg?raw'
  import replyAllIcon from '@phosphor-icons/core/assets/duotone/arrow-bend-double-up-left-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import { openReplyWith, replyRecipients, replySession } from '../../stores/compose'
  import { sendSession } from '../../lib/composeactions'
  import { prefs } from '../../stores/prefs'
  import { displayName } from '../../lib/format'
  import { t } from '../../lib/i18n'
  import type { EditorMode, MessageDetail } from '../../lib/types'

  /** The message a reply answers; null while it loads. */
  export let detail: MessageDetail | null

  let text = ''
  let all = false
  let sending = false
  let focused = false
  let box: HTMLTextAreaElement

  $: mode = $prefs.defaultEditorMode as EditorMode
  $: name = detail ? displayName(detail.fromName, detail.fromAddress) : ''
  // reply all is only offered when it would reach someone a reply does not.
  $: canReplyAll = detail ? widens(detail) : false

  function widens(source: MessageDetail): boolean {
    const everyone = replyRecipients(source, true)
    return everyone.cc !== '' || everyone.to !== replyRecipients(source, false).to
  }
  $: if (!canReplyAll) {
    all = false
  }
  $: open = focused || text !== ''

  async function send(): Promise<void> {
    if (!detail || text.trim() === '' || sending) {
      return
    }
    sending = true
    const session = replySession(detail, mode, all ? 'replyAll' : 'reply', text)
    const queued = await sendSession(session.id, session)
    sending = false
    if (queued) {
      text = ''
      box?.blur()
    }
  }

  function expand(): void {
    if (!detail) {
      return
    }
    openReplyWith(detail, mode, all ? 'replyAll' : 'reply', text)
    text = ''
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
      event.preventDefault()
      void send()
    } else if (event.key === 'Escape') {
      event.stopPropagation()
      box.blur()
    }
  }
</script>

<div class="quick" class:open>
  <textarea
    bind:this={box}
    bind:value={text}
    rows="1"
    dir="auto"
    aria-label={$t('reader.quickReply.label')}
    placeholder={all ? $t('reader.quickReply.placeholderAll') : $t('reader.quickReply.placeholder').replace('{name}', name)}
    disabled={!detail || sending}
    on:focus={() => (focused = true)}
    on:blur={() => (focused = false)}
    on:keydown={onKeydown}
  ></textarea>
  <div class="row">
    {#if canReplyAll}
      <button
        type="button"
        class="toggle"
        aria-pressed={all}
        title={$t('shortcut.replyAll')}
        on:mousedown|preventDefault
        on:click={() => (all = !all)}
      >
        <Ph svg={replyAllIcon} size={14} />
        <span>{$t('shortcut.replyAll')}</span>
      </button>
    {/if}
    <span class="spacer"></span>
    <button
      type="button"
      class="icon"
      aria-label={$t('reader.quickReply.expand')}
      title={$t('reader.quickReply.expand')}
      disabled={!detail}
      on:click={expand}
    >
      <Ph svg={expandIcon} size={16} />
    </button>
    <button type="button" class="send" disabled={!detail || sending || text.trim() === ''} on:click={send}>
      {#if sending}
        <PieProgress size={14} label={$t('reader.quickReply.sending')} />
      {:else}
        <Ph svg={sendIcon} size={14} />
      {/if}
      <span>{$t('reader.quickReply.send')}</span>
    </button>
  </div>
</div>

<style>
  .quick {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin-top: var(--space-4);
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    transition: border-color var(--motion-fast) var(--ease-out);
  }

  .quick.open {
    border-color: var(--border-strong);
  }

  textarea {
    min-height: calc(var(--control-height) - var(--space-2));
    padding: var(--space-1) var(--space-2);
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
    line-height: 1.5;
    resize: none;
    outline: none;
    transition: min-height var(--motion-base) var(--ease-out);
  }

  .open textarea {
    min-height: calc(var(--control-height) * 3);
  }

  textarea::placeholder {
    color: var(--text-tertiary);
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .spacer {
    flex: 1;
  }

  .toggle,
  .icon,
  .send {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: calc(var(--control-height) - var(--space-2));
    padding: 0 var(--space-3);
    border: var(--hairline) solid transparent;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      border-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .icon {
    padding: 0 var(--space-2);
  }

  .toggle:hover,
  .icon:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .toggle[aria-pressed='true'] {
    border-color: var(--border-strong);
    background: var(--surface-sunken);
    color: var(--text-primary);
  }

  .send {
    background: var(--accent);
    color: var(--accent-fg);
    font-weight: var(--fw-medium);
  }

  .send:disabled {
    background: var(--surface-sunken);
    color: var(--text-tertiary);
    cursor: default;
  }

  .icon:disabled {
    cursor: default;
    color: var(--text-tertiary);
  }

  @media (prefers-reduced-motion: reduce) {
    textarea {
      transition: none;
    }
  }
</style>
