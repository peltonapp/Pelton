<script lang="ts">
  // a weekday by hour grid of received mail, drawn as plain svg. A cell is
  // darker the more mail arrived in that hour; hovering one shows its count.
  // A screen reader gets the busiest weekday and hour in the label and the
  // whole grid as a table.
  import { tip } from '../../lib/tip'

  /** Rows of 24 counts, one row per entry of `days`. */
  export let grid: number[][]
  /** Short weekday names, one per row. */
  export let days: string[]
  /** Accessible name of the chart. */
  export let label: string
  /** Words one cell's tooltip from its weekday name, hour and count. */
  export let cellText: (day: string, hour: number, count: number) => string

  const cell = 10
  const gap = 1.5
  const hourMarks = [0, 6, 12, 18]

  $: max = Math.max(1, ...grid.flat())

  // five steps rather than a continuous ramp, so neighbouring cells read as
  // equal or not at a glance.
  function level(n: number): number {
    return n === 0 ? 0 : Math.min(5, Math.ceil((n / max) * 5))
  }
</script>

<figure class="heat">
  <div class="rows">
    {#each days as day (day)}
      <span class="day">{day}</span>
    {/each}
  </div>
  <div class="plot">
    <svg viewBox={`0 0 ${24 * cell} ${days.length * cell}`} role="img" aria-label={label}>
      {#each grid as row, r (r)}
        {#each row as count, h (h)}
          <rect
            class={`l${level(count)}`}
            data-testid="cell"
            x={h * cell + gap / 2}
            y={r * cell + gap / 2}
            width={cell - gap}
            height={cell - gap}
            rx="1.2"
            use:tip={{ text: cellText(days[r], h, count), side: 'top' }}
          />
        {/each}
      {/each}
    </svg>
    <div class="hours" aria-hidden="true">
      {#each hourMarks as h (h)}
        <span style={`inset-inline-start:${(h / 24) * 100}%`}>{String(h).padStart(2, '0')}</span>
      {/each}
    </div>
  </div>
  <table class="sr">
    <caption>{label}</caption>
    <tbody>
      {#each grid as row, r (r)}
        <tr><th scope="row">{days[r]}</th>{#each row as count, h (h)}<td>{h}: {count}</td>{/each}</tr>
      {/each}
    </tbody>
  </table>
</figure>

<style>
  .heat {
    margin: 0;
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: var(--space-3);
    align-items: stretch;
  }

  .rows {
    display: grid;
    grid-template-rows: repeat(7, 1fr);
    box-sizing: border-box;
    padding-bottom: calc(var(--fz-meta) + var(--space-2));
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .day {
    display: flex;
    align-items: center;
    line-height: 1;
  }

  .plot {
    position: relative;
    padding-bottom: calc(var(--fz-meta) + var(--space-2));
  }

  svg {
    display: block;
    width: 100%;
    height: auto;
  }

  rect {
    fill: var(--text-primary);
    stroke: none;
  }

  .l0 {
    fill: var(--surface-sunken);
  }

  .l1 {
    fill-opacity: 0.12;
  }

  .l2 {
    fill-opacity: 0.28;
  }

  .l3 {
    fill-opacity: 0.45;
  }

  .l4 {
    fill-opacity: 0.65;
  }

  .l5 {
    fill-opacity: 0.88;
  }

  rect:hover {
    stroke: var(--text-primary);
    stroke-width: 0.8;
  }

  .hours {
    position: absolute;
    inset-inline: 0;
    bottom: 0;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .hours span {
    position: absolute;
  }

  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
</style>
