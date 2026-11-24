<script lang="ts">
  // the follow-ups that came due without a reply, on top of the inbox. each
  // is a row of its own with a plain "Follow up" label: what was sent, to
  // whom and when. a click opens the sent message where the reader is, and
  // the cross dismisses the reminder.
  import { slide } from 'svelte/transition'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { dropFollowUp } from '../../stores/followups'
  import { openMessageId } from '../../stores/selection'
  import { prefs } from '../../stores/prefs'
  import { formatListDate, type TimeFormat } from '../../lib/format'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { FollowUp } from '../../lib/types'

  /** The fired reminders to show, the most recent first. */
  export let items: FollowUp[] = []

  function open(item: FollowUp): void {
    if (item.localId !== 0) {
      openMessageId.set(item.localId)
    }
  }
</script>

{#if items.length > 0}
  <ul class="band" aria-label={$t('followUp.band')} transition:slide={{ duration: dur(motion.base) }}>
    {#each items as item (item.id)}
      <li class="item" transition:slide={{ duration: dur(motion.base) }}>
        <button
          type="button"
          class="open"
          class:active={item.localId !== 0 && $openMessageId === item.localId}
          disabled={item.localId === 0}
          on:click={() => open(item)}
        >
          <span class="label">{$t('followUp.label')}</span>
          <span class="text">
            <span class="subject">{item.subject || $t('messageList.noSubject')}</span>
            <span class="meta">
              {item.recipients
                ? $t('followUp.noReplyFrom').replace('{who}', item.recipients)
                : $t('followUp.noReply')}
            </span>
          </span>
          <time class="date" datetime={item.sentAt}>{formatListDate(item.sentAt, $prefs.timeFormat as TimeFormat)}</time>
        </button>
        <button
          type="button"
          class="dismiss"
          aria-label={$t('followUp.dismiss')}
          title={$t('followUp.dismiss')}
          on:click={() => void dropFollowUp(item.id)}
        >
          <Ph svg={xIcon} size={12} />
        </button>
      </li>
    {/each}
  </ul>
{/if}

<style>
  .band {
    margin: 0;
    padding: 0;
    list-style: none;
    border-bottom: var(--hairline) solid var(--border-default);
    background: var(--surface-raised);
  }

  .item {
    display: flex;
    align-items: stretch;
  }

  .item + .item {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .open {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex: 1;
    min-width: 0;
    padding: var(--row-pad-y) var(--row-pad-x);
    border: none;
    background: transparent;
    color: var(--text-primary);
    font-family: var(--font-ui);
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .open:hover:not(:disabled) {
    background: var(--surface-hover);
  }

  .open.active {
    background: var(--selection-bg);
  }

  .open:disabled {
    cursor: default;
  }

  .label {
    flex-shrink: 0;
    padding: 1px var(--space-2);
    border: var(--hairline) solid var(--warning);
    border-radius: var(--radius-control);
    color: var(--warning);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    white-space: nowrap;
  }

  .text {
    display: flex;
    flex-direction: column;
    gap: var(--row-gap);
    min-width: 0;
    flex: 1;
  }

  .subject,
  .meta {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .subject {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
  }

  .meta {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .date {
    flex-shrink: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .dismiss {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--control-height);
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .dismiss:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
</style>
