<script lang="ts">
  // the focus schedule on the Notifications page: whether focus starts on its
  // own, on which days, and from when to when.
  import { onMount } from 'svelte'
  import Row from './ui/Row.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import { getFocusSchedule, setFocusSchedule } from '../../lib/api'
  import { errorMessage, toastError } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { FocusSchedule } from '../../lib/types'

  let schedule: FocusSchedule = { enabled: false, days: [1, 2, 3, 4, 5], start: '09:00', end: '12:00' }
  let loaded = false

  // Monday first; the stored numbers are 0 for Sunday to 6 for Saturday.
  const order = [1, 2, 3, 4, 5, 6, 0]
  // 2026-11-15 was a Sunday, so day n is the 15th plus n.
  const dayName = (day: number, style: 'short' | 'long'): string =>
    new Intl.DateTimeFormat(undefined, { weekday: style }).format(new Date(2026, 10, 15 + day))

  onMount(async () => {
    try {
      schedule = await getFocusSchedule()
    } catch {
      // keep the default shown.
    }
    loaded = true
  })

  async function save(next: FocusSchedule): Promise<void> {
    schedule = next
    try {
      await setFocusSchedule(next)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function toggleDay(day: number): void {
    const days = schedule.days.includes(day) ? schedule.days.filter((d) => d !== day) : [...schedule.days, day]
    void save({ ...schedule, days })
  }

  function setTime(which: 'start' | 'end', value: string): void {
    if (/^\d{2}:\d{2}$/.test(value)) {
      void save({ ...schedule, [which]: value })
    }
  }
</script>

<Row id="focus-schedule" label={$t('focus.schedule.label')} hint={$t('focus.schedule.hint')}>
  <ToggleSwitch
    checked={schedule.enabled}
    disabled={!loaded}
    label={$t('focus.schedule.label')}
    on:change={(e) => void save({ ...schedule, enabled: e.detail })}
  />
</Row>
<Row id="focus-days" label={$t('focus.schedule.days')} disabled={!schedule.enabled}>
  <div class="days" role="group" aria-label={$t('focus.schedule.days')}>
    {#each order as day (day)}
      <button
        type="button"
        class="day"
        class:on={schedule.days.includes(day)}
        aria-pressed={schedule.days.includes(day)}
        aria-label={dayName(day, 'long')}
        disabled={!schedule.enabled}
        on:click={() => toggleDay(day)}
      >
        {dayName(day, 'short')}
      </button>
    {/each}
  </div>
</Row>
<Row id="focus-hours" label={$t('focus.schedule.hours')} hint={$t('focus.schedule.hoursHint')} disabled={!schedule.enabled}>
  <div class="hours">
    <input
      type="time"
      aria-label={$t('focus.schedule.from')}
      value={schedule.start}
      disabled={!schedule.enabled}
      on:change={(e) => setTime('start', e.currentTarget.value)}
    />
    <span>{$t('focus.schedule.to')}</span>
    <input
      type="time"
      aria-label={$t('focus.schedule.until')}
      value={schedule.end}
      disabled={!schedule.enabled}
      on:change={(e) => setTime('end', e.currentTarget.value)}
    />
  </div>
</Row>

<style>
  .days {
    display: inline-flex;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    overflow: hidden;
  }

  .day {
    min-width: calc(var(--control-height) + var(--space-2));
    height: calc(var(--control-height) - var(--space-2));
    padding: 0 var(--space-2);
    border: none;
    border-inline-start: var(--hairline) solid var(--border-subtle);
    background: var(--surface-raised);
    color: var(--text-secondary);
    font-family: inherit;
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .day:first-child {
    border-inline-start: none;
  }

  .day:hover:not(:disabled) {
    background: var(--surface-hover);
  }

  .day.on {
    background: var(--selection-bg);
    color: var(--text-primary);
    font-weight: var(--fw-medium);
  }

  .day:disabled {
    cursor: default;
  }

  .hours {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .hours input {
    height: calc(var(--control-height) - var(--space-2));
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-family: inherit;
    font-size: var(--fz-label);
  }
</style>
