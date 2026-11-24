<script lang="ts">
  // the question a send stops on, as a bar above the text rather than a
  // dialog, so the message stays in view and editable while it is answered.
  // one issue at a time, the most pressing first.
  import { createEventDispatcher, onMount } from 'svelte'
  import warnIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import clipIcon from '@phosphor-icons/core/assets/duotone/paperclip-duotone.svg?raw'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import { t } from '../../../lib/i18n'
  import type { SendIssue } from '../../../lib/sendcheck'

  /** What the send stopped on. */
  export let issue: SendIssue

  const dispatch = createEventDispatcher<{
    sendAnyway: void
    attach: void
    fix: void
    neverAsk: void
    dismiss: void
  }>()

  let bar: HTMLDivElement

  // the first answer takes focus, so the bar is answered from the keyboard.
  onMount(() => bar?.querySelector<HTMLButtonElement>('button')?.focus())

  $: message =
    issue === 'noRecipients'
      ? $t('sendCheck.noRecipients')
      : issue === 'missingAttachment'
        ? $t('sendCheck.missingAttachment')
        : $t('sendCheck.emptySubject')

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      dispatch('dismiss')
    }
  }
</script>

<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
<div
  class="bar"
  role="alert"
  bind:this={bar}
  on:keydown={onKeydown}
>
  <span class="icon"><Ph svg={warnIcon} size={16} /></span>
  <span class="text">{message}</span>
  <span class="actions">
    {#if issue === 'missingAttachment'}
      <button type="button" class="c2-tool" on:click={() => dispatch('attach')}>
        <Ph svg={clipIcon} size={14} />
        <span>{$t('sendCheck.attach')}</span>
      </button>
      <button type="button" class="c2-tool" on:click={() => dispatch('sendAnyway')}>{$t('sendCheck.sendAnyway')}</button>
      <button type="button" class="link" on:click={() => dispatch('neverAsk')}>{$t('sendCheck.neverAsk')}</button>
    {:else if issue === 'emptySubject'}
      <button type="button" class="c2-tool" on:click={() => dispatch('fix')}>{$t('sendCheck.addSubject')}</button>
      <button type="button" class="c2-tool" on:click={() => dispatch('sendAnyway')}>{$t('sendCheck.sendAnyway')}</button>
    {:else}
      <button type="button" class="c2-tool" on:click={() => dispatch('fix')}>{$t('sendCheck.addRecipient')}</button>
    {/if}
    <button type="button" class="c2-tool compact" aria-label={$t('sendCheck.dismiss')} on:click={() => dispatch('dismiss')}>
      <Ph svg={xIcon} size={13} />
    </button>
  </span>
</div>

<style>
  .bar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--space-2) var(--space-3);
    margin: var(--space-2) var(--pane-pad) 0;
    padding: var(--space-2) var(--space-2) var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--warning);
    border-inline-start-width: 3px;
    border-radius: var(--radius-control);
    background: var(--warning-bg);
    color: var(--text-primary);
    font-size: var(--fz-label);
    animation: drop var(--motion-base) var(--ease-out);
  }

  @keyframes drop {
    from {
      opacity: 0;
      transform: translateY(calc(-1 * var(--space-2)));
    }
  }

  .icon {
    display: inline-flex;
    color: var(--warning);
  }

  .text {
    flex: 1;
    min-width: 0;
  }

  .actions {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    flex-wrap: wrap;
  }

  .link {
    padding: var(--space-1) var(--space-2);
    border: none;
    background: transparent;
    color: var(--text-secondary);
    font-family: var(--font-ui);
    font-size: var(--fz-meta);
    text-decoration: underline;
    text-underline-offset: 2px;
    cursor: var(--cursor-action);
  }

  .link:hover {
    color: var(--text-primary);
  }
</style>
