<script lang="ts">
  // the two places a time is read: on a message row and in the status bar's
  // clock, both written in the chosen clock format.
  import Frame from './Frame.svelte'
  import { formatListDate, formatStatusClock, type TimeFormat } from '../../../lib/format'
  import { sampleMessages } from './samples'

  /** The clock format to draw: auto follows the system. */
  export let format: TimeFormat = 'auto'

  const person = sampleMessages[0]

  // an afternoon time shows the difference between 12 and 24 hour clocks.
  $: moment = (() => {
    const d = new Date()
    d.setHours(14, 5, 0, 0)
    return d
  })()
  $: rowTime = formatListDate(moment.toISOString(), format)
  $: clock = formatStatusClock(moment, format)
</script>

<Frame>
  <div class="row">
    <span class="from">{person.name}</span>
    <span class="subject">{person.subject}</span>
    <span class="time">{rowTime}</span>
  </div>
  <div class="status">
    <span class="clock">{clock}</span>
  </div>
</Frame>

<style>
  .row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding-bottom: var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
    font-size: var(--fz-label);
  }

  .from {
    flex-shrink: 0;
    color: var(--text-primary);
    font-weight: var(--fw-semibold);
  }

  .subject {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-tertiary);
  }

  .time,
  .clock {
    flex-shrink: 0;
    font-size: var(--fz-meta);
    font-variant-numeric: tabular-nums;
    color: var(--text-secondary);
  }

  .status {
    display: flex;
    justify-content: flex-end;
    padding-top: var(--space-3);
  }
</style>
