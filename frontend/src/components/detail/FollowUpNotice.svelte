<script lang="ts">
  // a sent message with a follow-up reminder says so under its header: when
  // the reminder comes if nobody replies, or that it came and nobody did, with
  // a way to cancel or dismiss it.
  import { slide } from 'svelte/transition'
  import bellIcon from '@phosphor-icons/core/assets/duotone/bell-ringing-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { followUps, followUpFor, dropFollowUp } from '../../stores/followups'
  import { formatSendWhen } from '../../lib/composeactions'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'

  /** The Message-ID of the message shown. */
  export let messageId: string

  $: item = followUpFor($followUps, messageId)
  $: fired = item !== null && item.firedAt !== ''
</script>

{#if item}
  <div class="notice" class:fired role="status" transition:slide={{ duration: dur(motion.base) }}>
    <Ph svg={bellIcon} size={14} />
    <span class="text">
      {fired ? $t('followUp.notice.fired') : $t('followUp.notice.waiting').replace('{when}', formatSendWhen(new Date(item.dueAt)))}
    </span>
    <button type="button" class="act" on:click={() => item && void dropFollowUp(item.id)}>
      {fired ? $t('followUp.dismiss') : $t('followUp.cancel')}
    </button>
  </div>
{/if}

<style>
  .notice {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0 var(--pane-pad) var(--space-3);
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-secondary);
    font-size: var(--fz-label);
  }

  .notice.fired {
    border-color: var(--warning);
    color: var(--text-primary);
  }

  .notice.fired :global(.ph) {
    color: var(--warning);
  }

  .text {
    flex: 1;
    min-width: 0;
  }

  .act {
    flex-shrink: 0;
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .act:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
</style>
