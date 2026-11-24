<script lang="ts">
  // a change the server refused because its copy of the event moved on: both
  // versions side by side, and a choice of which one stays.
  import '../../theme/controls.css'
  import Modal from '../common/Modal.svelte'
  import { eventConflict, resolveConflict, dismissEventConflict } from '../../stores/calendar'
  import { parseEventTime } from '../../lib/caldate'
  import { formatWhen } from '../../lib/calformat'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { CalendarEvent } from '../../lib/types'

  export let timeFormat: TimeFormat = 'auto'

  function when(ev: CalendarEvent): string {
    if (!ev.start) {
      return ''
    }
    return formatWhen(parseEventTime(ev.start, ev.allDay), parseEventTime(ev.end, ev.allDay), ev.allDay, timeFormat)
  }
</script>

{#if $eventConflict}
  {@const mine = $eventConflict.event}
  {@const theirs = $eventConflict.server}
  <Modal title={$t('calendar.conflict.title')} hint={$t('calendar.conflict.hint')} size="medium" on:close={dismissEventConflict}>
    <div class="sides">
      <section>
        <h4>{$t('calendar.conflict.mine')}</h4>
        <p class="what">{mine.summary || $t('calendar.untitled')}</p>
        <p class="detail">{when(mine)}</p>
        {#if mine.location}<p class="detail">{mine.location}</p>{/if}
      </section>
      <section>
        <h4>{$t('calendar.conflict.theirs')}</h4>
        {#if theirs.id || theirs.summary || theirs.start}
          <p class="what">{theirs.summary || $t('calendar.untitled')}</p>
          <p class="detail">{when(theirs)}</p>
          {#if theirs.location}<p class="detail">{theirs.location}</p>{/if}
        {:else}
          <p class="detail">{$t('calendar.conflict.deleted')}</p>
        {/if}
      </section>
    </div>
    <svelte:fragment slot="footer">
      <div class="foot">
        <button type="button" class="cal-btn" on:click={() => resolveConflict(false)}>{$t('calendar.conflict.useTheirs')}</button>
        <button type="button" class="cal-btn primary" on:click={() => resolveConflict(true)}>{$t('calendar.conflict.keepMine')}</button>
      </div>
    </svelte:fragment>
  </Modal>
{/if}

<style>
  .sides {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(calc(var(--cal-popover-width) * 0.6), 1fr));
    gap: var(--space-4);
  }

  section {
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  h4 {
    margin: 0 0 var(--space-3);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  p {
    margin: 0 0 var(--space-1);
  }

  .what {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
  }

  .detail {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .foot {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    width: 100%;
  }
</style>
