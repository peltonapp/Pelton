<script lang="ts">
  // what a task made from a promise says about it: the sentence it came from
  // and who owes it, that a reply closed it (with undo), that the user
  // replied (with an offer to complete it), or, once it is overdue and owed
  // to the user, a gentle offer to write a reminder. a reminder is only ever
  // a draft; nothing is sent from here.
  import handshakeIcon from '@phosphor-icons/core/assets/duotone/handshake-duotone.svg?raw'
  import '../../theme/controls.css'
  import Ph from '../common/Ph.svelte'
  import { nudge, promiseByTask, promiseSettings, reopen } from '../../stores/promises'
  import { toggleDone } from '../../stores/tasks'
  import { prefs } from '../../stores/prefs'
  import { isOverdue } from '../../lib/tasklogic'
  import { openMailLink } from '../../lib/maillink'
  import { ownerKey, personOf, shortDate } from '../../lib/promisetext'
  import { locale, t } from '../../lib/i18n'
  import type { EditorMode, Task } from '../../lib/types'

  /** The task shown. */
  export let task: Task
  /** The current time, for whether it is overdue. */
  export let now: Date = new Date()

  $: p = $promiseByTask.get(task.uid) ?? null
  $: closed = p?.state === 'closed'
  $: offered = p !== null && p.direction === 'mine' && p.replyId !== '' && !task.done
  $: nudging = p !== null && p.direction === 'theirs' && p.state === 'accepted' && $promiseSettings.nudge && !p.followUp && p.messageId > 0 && isOverdue(task, now)

  let announcement = ''

  async function undoClose(): Promise<void> {
    if (p) {
      await reopen(p)
      announcement = $t('promises.reopened')
    }
  }

  async function complete(): Promise<void> {
    await toggleDone(task, true)
    announcement = $t('promises.completed')
  }

  function openReply(): void {
    if (p?.replyId) {
      openMailLink(`mid:${encodeURIComponent(p.replyId)}`)
    }
  }
</script>

<p class="sr-only" aria-live="polite">{announcement}</p>
{#if p}
  <section class="note" aria-label={$t('promises.task.label')}>
    <span class="icon" aria-hidden="true"><Ph svg={handshakeIcon} size={16} /></span>
    <div class="body">
      <p class="who">{$t(ownerKey(p)).replace('{name}', personOf(p))}</p>
      <q class="quote">{p.quote}</q>
      {#if closed}
        <p class="state">{$t('promises.task.closed').replace('{name}', personOf(p)).replace('{when}', shortDate(p.replyAt, $locale))}</p>
        <div class="acts">
          {#if p.replyMessageId}
            <button type="button" class="cal-btn" on:click={openReply}>{$t('promises.task.openReply')}</button>
          {/if}
          <button type="button" class="cal-btn" on:click={() => void undoClose()}>{$t('promises.undo')}</button>
        </div>
      {:else if offered}
        <p class="state">{$t('promises.task.replied').replace('{when}', shortDate(p.replyAt, $locale))}</p>
        <div class="acts">
          <button type="button" class="cal-btn primary" on:click={() => void complete()}>{$t('promises.task.markDone')}</button>
          <button type="button" class="cal-btn" on:click={() => p && void reopen(p)}>{$t('promises.task.notYet')}</button>
        </div>
      {:else if nudging}
        <p class="state">{$t('promises.nudge').replace('{name}', personOf(p)).replace('{when}', shortDate(p.mailDate.slice(0, 10), $locale))}</p>
        <div class="acts">
          <button type="button" class="cal-btn" on:click={() => p && void nudge(p, $prefs.defaultEditorMode as EditorMode)}>{$t('promises.nudge.write')}</button>
        </div>
      {/if}
    </div>
  </section>
{/if}

<style>
  .note {
    display: flex;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border-left: var(--hairline) solid var(--accent);
    background: var(--surface-sunken);
    border-radius: var(--radius-control);
    color: var(--text-secondary);
    font-size: var(--fz-label);
  }

  .icon {
    display: inline-flex;
    color: var(--accent);
  }

  .body {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .who,
  .state {
    margin: 0;
  }

  .who {
    color: var(--text-primary);
    font-weight: var(--fw-medium);
  }

  .acts {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }
</style>
