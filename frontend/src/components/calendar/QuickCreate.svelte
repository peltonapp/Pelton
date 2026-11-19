<script lang="ts">
  // the small form a drawn or clicked slot opens: a title, the time it was
  // drawn for and the calendar it goes in, in the rows of the full editor.
  // Enter saves; "More options" takes what was typed into the full editor.
  import { createEventDispatcher, onMount } from 'svelte'
  import clockIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import calendarIcon from '@phosphor-icons/core/assets/duotone/calendar-blank-duotone.svg?raw'
  import './controls.css'
  import Ph from '../common/Ph.svelte'
  import Select from '../common/Select.svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import { calendarColor, formatWhen } from '../../lib/calformat'
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

  $: items = calendars.map((c) => ({ value: String(c.id), label: c.name || $t('calendar.defaultName'), color: calendarColor(c) }))

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
    <input class="title" bind:this={input} bind:value={summary} placeholder={$t('calendar.addTitle')} aria-label={$t('calendar.title')} />
    <div class="row">
      <span class="icon" aria-hidden="true"><Ph svg={clockIcon} size={15} /></span>
      <span class="when">{formatWhen(start, end, allDay, timeFormat)}</span>
    </div>
    {#if items.length > 1}
      <div class="row">
        <span class="icon" aria-hidden="true"><Ph svg={calendarIcon} size={15} /></span>
        <Select class="ctl" value={String(calendarId)} {items} ariaLabel={$t('calendar.calendar')} on:change={(e) => (calendarId = Number(e.detail))} />
      </div>
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
    width: var(--cal-popover-width);
    max-width: 100%;
    padding: var(--space-2) var(--space-4) var(--space-3);
  }

  .title {
    padding: var(--space-2) 0;
    border: none;
    border-bottom: var(--hairline) solid var(--border-subtle);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-heading);
    font-weight: var(--fw-medium);
  }

  .title:focus {
    outline: none;
    border-bottom-color: var(--accent);
  }

  .title::placeholder {
    color: var(--text-tertiary);
  }

  .row {
    display: grid;
    grid-template-columns: var(--space-5) minmax(0, 1fr);
    align-items: center;
    column-gap: var(--space-2);
    min-height: var(--control-height);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .icon {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  .when {
    font-size: var(--fz-list);
    color: var(--text-secondary);
    font-variant-numeric: tabular-nums;
  }

  .quick :global(.ctl) {
    justify-self: start;
    height: calc(var(--control-height) - 4px);
    padding: 0 var(--space-2);
    margin-inline-start: calc(-1 * var(--space-2));
    border-color: transparent;
    background: transparent;
    font-size: var(--fz-list);
  }

  .quick :global(.ctl:hover),
  .quick :global(.ctl.open) {
    border-color: var(--border-default);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    margin-top: var(--space-3);
  }
</style>
