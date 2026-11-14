<script lang="ts">
  // the small form a drawn or clicked slot opens: a title, the time it was
  // drawn for and the calendar it goes in. Enter saves; "More options" takes
  // what was typed into the full editor.
  import { createEventDispatcher, onMount } from 'svelte'
  import clockIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import './controls.css'
  import Ph from '../common/Ph.svelte'
  import Select from '../common/Select.svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import { formatWhen } from '../../lib/calformat'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { Calendar } from '../../lib/types'

  /** The element it hangs off. */
  export let anchor: HTMLElement
  export let start: Date
  export let end: Date
  export let allDay: boolean
  /** Calendars an event can go in, and the one chosen. */
  export let calendars: Calendar[]
  export let calendarId: number
  export let timeFormat: TimeFormat = 'auto'

  const dispatch = createEventDispatcher<{
    close: void
    save: { summary: string; calendarId: number }
    more: { summary: string; calendarId: number }
  }>()

  let summary = ''
  let input: HTMLInputElement
  let saving = false

  $: items = calendars.map((c) => ({ value: String(c.id), label: c.name || $t('calendar.defaultName') }))

  onMount(() => input?.focus())

  function save(): void {
    if (saving) {
      return
    }
    saving = true
    dispatch('save', { summary: summary.trim(), calendarId })
  }
</script>

<AnchoredPopover {anchor} align="start" label={$t('calendar.newEvent')} on:close>
  <form class="quick" on:submit|preventDefault={save}>
    <input
      class="cal-field title"
      bind:this={input}
      bind:value={summary}
      placeholder={$t('calendar.addTitle')}
      aria-label={$t('calendar.title')}
    />
    <p class="when"><Ph svg={clockIcon} size={14} />{formatWhen(start, end, allDay, timeFormat)}</p>
    {#if items.length > 1}
      <Select value={String(calendarId)} {items} ariaLabel={$t('calendar.calendar')} on:change={(e) => (calendarId = Number(e.detail))} />
    {/if}
    <div class="actions">
      <button type="button" class="cal-btn" on:click={() => dispatch('more', { summary: summary.trim(), calendarId })}>
        {$t('calendar.moreOptions')}
      </button>
      <button type="submit" class="cal-btn primary" disabled={saving}>{$t('calendar.save')}</button>
    </div>
  </form>
</AnchoredPopover>

<style>
  .quick {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    width: var(--cal-popover-width);
    padding: var(--space-4);
  }

  .title {
    height: calc(var(--control-height) + var(--space-2));
    font-size: var(--fz-body);
  }

  .when {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    margin-top: var(--space-1);
  }
</style>
