<script lang="ts">
  // the conversation reader's floating action bar. It sticks to the bottom of
  // the reading pane while the conversation scrolls under it, so the common
  // actions stay a click away however long the thread is.
  import { createEventDispatcher } from 'svelte'
  import { fly } from 'svelte/transition'
  import replyIcon from '@phosphor-icons/core/assets/duotone/arrow-bend-up-left-duotone.svg?raw'
  import replyAllIcon from '@phosphor-icons/core/assets/duotone/arrow-bend-double-up-left-duotone.svg?raw'
  import forwardIcon from '@phosphor-icons/core/assets/duotone/arrow-bend-up-right-duotone.svg?raw'
  import archiveIcon from '@phosphor-icons/core/assets/duotone/archive-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import moreIcon from '@phosphor-icons/core/assets/duotone/dots-three-duotone.svg?raw'
  import pinIcon from '@phosphor-icons/core/assets/duotone/push-pin-duotone.svg?raw'
  import unpinIcon from '@phosphor-icons/core/assets/duotone/push-pin-slash-duotone.svg?raw'
  import muteIcon from '@phosphor-icons/core/assets/duotone/bell-slash-duotone.svg?raw'
  import unmuteIcon from '@phosphor-icons/core/assets/duotone/bell-duotone.svg?raw'
  import noteIcon from '@phosphor-icons/core/assets/duotone/note-pencil-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { shortcutTitle } from '../../stores/shortcuts'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'

  /** The message, or its conversation, is pinned. */
  export let pinned: boolean = false
  /** The conversation is muted. */
  export let muted: boolean = false
  /** Pin acts on the whole conversation. */
  export let conversation: boolean = false
  /** The message belongs to a conversation that can be muted. */
  export let canMute: boolean = true

  $: pinLabel = conversation
    ? $t(pinned ? 'pin.unpinConversation' : 'pin.pinConversation')
    : $t(pinned ? 'pin.unpin' : 'pin.pin')
  $: muteLabel = $t(muted ? 'mute.unmute' : 'mute.mute')

  const dispatch = createEventDispatcher<{
    reply: void
    replyAll: void
    forward: void
    archive: void
    delete: void
    pin: void
    mute: void
    note: void
    more: { x: number; y: number }
  }>()

  function onMore(event: MouseEvent): void {
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
    dispatch('more', { x: rect.left, y: rect.top })
  }
</script>

<div class="dock">
  <div class="bar" role="toolbar" aria-label={$t('reader.actions')} in:fly={{ y: 8, duration: dur(motion.slow) }}>
    <button type="button" class="btn primary" title={$shortcutTitle($t('action.reply'), 'reply')} on:click={() => dispatch('reply')}>
      <Ph svg={replyIcon} size={16} />
      <span>{$t('action.reply')}</span>
    </button>
    <button
      type="button"
      class="btn"
      aria-label={$t('shortcut.replyAll')}
      title={$shortcutTitle($t('shortcut.replyAll'), 'reply-all')}
      on:click={() => dispatch('replyAll')}
    >
      <Ph svg={replyAllIcon} size={16} />
    </button>
    <button
      type="button"
      class="btn"
      aria-label={$t('action.forward')}
      title={$shortcutTitle($t('action.forward'), 'forward')}
      on:click={() => dispatch('forward')}
    >
      <Ph svg={forwardIcon} size={16} />
    </button>
    <span class="sep" aria-hidden="true"></span>
    <button
      type="button"
      class="btn"
      aria-label={$t('action.archive')}
      title={$shortcutTitle($t('action.archive'), 'archive')}
      on:click={() => dispatch('archive')}
    >
      <Ph svg={archiveIcon} size={16} />
    </button>
    <button
      type="button"
      class="btn danger"
      aria-label={$t('action.delete')}
      title={$shortcutTitle($t('action.delete'), 'delete-message')}
      on:click={() => dispatch('delete')}
    >
      <Ph svg={trashIcon} size={16} />
    </button>
    <span class="sep" aria-hidden="true"></span>
    <button
      type="button"
      class="btn"
      class:on={pinned}
      aria-label={pinLabel}
      aria-pressed={pinned}
      title={$shortcutTitle(pinLabel, 'toggle-pin')}
      on:click={() => dispatch('pin')}
    >
      <Ph svg={pinned ? unpinIcon : pinIcon} size={16} />
    </button>
    {#if canMute}
      <button
        type="button"
        class="btn"
        class:on={muted}
        aria-label={muteLabel}
        aria-pressed={muted}
        title={$shortcutTitle(muteLabel, 'toggle-mute')}
        on:click={() => dispatch('mute')}
      >
        <Ph svg={muted ? unmuteIcon : muteIcon} size={16} />
      </button>
    {/if}
    <button
      type="button"
      class="btn"
      aria-label={$t('note.add')}
      title={$shortcutTitle($t('note.add'), 'edit-note')}
      on:click={() => dispatch('note')}
    >
      <Ph svg={noteIcon} size={16} />
    </button>
    <button type="button" class="btn" aria-label={$t('reader.more')} title={$t('reader.more')} on:click={onMore}>
      <Ph svg={moreIcon} size={16} />
    </button>
  </div>
</div>

<style>
  /* the dock is a zero-height sticky strip, so the bar floats over the end of
     the conversation without taking room from it. */
  .dock {
    position: sticky;
    bottom: 0;
    z-index: 5;
    display: flex;
    align-items: flex-end;
    justify-content: center;
    height: 0;
    overflow: visible;
    pointer-events: none;
  }

  .bar {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    margin-bottom: var(--space-4);
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    pointer-events: auto;
  }

  .btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: calc(var(--control-height) - var(--space-1));
    padding: 0 var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .btn:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .btn:active {
    transform: scale(var(--press-scale));
  }

  .btn.primary {
    background: var(--accent);
    color: var(--accent-fg);
    font-weight: var(--fw-medium);
  }

  .btn.primary:hover {
    background: color-mix(in srgb, var(--accent) 88%, var(--text-primary));
    color: var(--accent-fg);
  }

  .btn.on {
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .btn.danger:hover {
    background: var(--danger-bg);
    color: var(--danger);
  }

  .sep {
    width: var(--hairline);
    align-self: stretch;
    margin: var(--space-1) var(--space-1);
    background: var(--border-default);
  }

  @media (prefers-reduced-motion: reduce) {
    .btn {
      transition: none;
    }
  }
</style>
