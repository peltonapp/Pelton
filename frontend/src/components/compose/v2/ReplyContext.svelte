<script lang="ts">
  // the one line above a reply's header that says what it answers, with the
  // reply type switchable in place (Reply, Reply all, Forward) and a button
  // that shows the original message in the main window.
  import { createEventDispatcher } from 'svelte'
  import replyIcon from '@phosphor-icons/core/assets/duotone/arrow-bend-up-left-duotone.svg?raw'
  import replyAllIcon from '@phosphor-icons/core/assets/duotone/arrow-bend-double-up-left-duotone.svg?raw'
  import forwardIcon from '@phosphor-icons/core/assets/duotone/arrow-bend-up-right-duotone.svg?raw'
  import showIcon from '@phosphor-icons/core/assets/duotone/arrow-square-out-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import { tip } from '../../../lib/tip'
  import { t } from '../../../lib/i18n'
  import type { ComposeOrigin, ReplyKind } from '../../../stores/compose'

  /** The message being answered or forwarded. */
  export let origin: ComposeOrigin

  const dispatch = createEventDispatcher<{ kind: ReplyKind; show: void }>()

  $: kinds = [
    { kind: 'reply', icon: replyIcon, label: $t('action.reply') },
    { kind: 'replyAll', icon: replyAllIcon, label: $t('detail.toolbar.replyAll') },
    { kind: 'forward', icon: forwardIcon, label: $t('action.forward') },
  ] as { kind: ReplyKind; icon: string; label: string }[]

  $: name = origin.fromName || origin.fromAddress
  $: subject = origin.subject || $t('detail.noSubject')
  $: line = (origin.kind === 'forward' ? $t('compose2.context.forwarding') : $t('compose2.context.replying'))
    .replace('{name}', name)
    .replace('{subject}', subject)
</script>

<div class="context">
  <div class="kinds" role="radiogroup" aria-label={$t('compose2.context.kind')}>
    {#each kinds as k (k.kind)}
      <button
        type="button"
        role="radio"
        class="kind"
        class:on={origin.kind === k.kind}
        aria-checked={origin.kind === k.kind}
        aria-label={k.label}
        use:tip={{ text: k.label, side: 'bottom' }}
        on:click={() => dispatch('kind', k.kind)}
      >
        <Ph svg={k.icon} size={14} />
      </button>
    {/each}
  </div>
  <span class="line" title={line}>{line}</span>
  <button type="button" class="show" on:click={() => dispatch('show')}>
    <Ph svg={showIcon} size={14} />
    <span>{$t('compose2.context.showOriginal')}</span>
  </button>
</div>

<style>
  .context {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--pane-pad);
    border-bottom: var(--hairline) solid var(--border-subtle);
    background: var(--surface-sunken);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .kinds {
    display: inline-flex;
    flex-shrink: 0;
    padding: var(--space-1);
    gap: var(--space-1);
    border-radius: var(--radius-control);
    background: var(--surface-base);
    box-shadow: inset 0 0 0 var(--hairline) var(--border-subtle);
  }

  .kind {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: calc(var(--control-height) - var(--space-4));
    height: calc(var(--control-height) - var(--space-4));
    border: none;
    border-radius: calc(var(--radius-control) - var(--space-1));
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .kind:hover {
    color: var(--text-primary);
  }

  .kind:active {
    transform: scale(var(--press-scale));
  }

  .kind.on {
    background: var(--surface-raised);
    color: var(--accent);
    box-shadow: 0 0 0 var(--hairline) var(--border-default);
  }

  .line {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .show {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    flex-shrink: 0;
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font-family: var(--font-ui);
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .show:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
</style>
