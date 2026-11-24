<script lang="ts">
  // a small picture, drawn from the theme tokens, of one calendar view: the
  // weeks of a month, the columns of a week, one day, or the agenda's list.
  // choice cards use it so the view the calendar opens in is picked by what it
  // looks like.
  import type { CalendarView } from '../../../lib/types'

  /** The view to draw. */
  export let view: CalendarView = 'week'
  /** Height of the picture in px. */
  export let height: number = 92

  // a few events, as [day, start row, rows, color] on a grid of eight rows.
  const weekEvents: [number, number, number, number][] = [
    [0, 1, 2, 1],
    [1, 3, 2, 2],
    [2, 1, 1, 3],
    [2, 4, 3, 1],
    [3, 2, 2, 4],
    [4, 1, 3, 2],
  ]
  const dayEvents: [number, number, number][] = [
    [1, 2, 1],
    [3, 1, 3],
    [5, 2, 2],
  ]
  const monthBars: [number, number, number, number][] = [
    [0, 1, 3, 1],
    [1, 4, 2, 2],
    [2, 0, 1, 3],
    [3, 2, 4, 4],
    [3, 6, 1, 1],
  ]
</script>

<div class="stage" style={`height:${height}px`} aria-hidden="true">
  <div class="bar"><i class="today"></i><i></i><i></i><span class="title"></span></div>
  {#if view === 'month'}
    <div class="month">
      {#each [0, 1, 2, 3] as w (w)}
        <div class="week">
          {#each [0, 1, 2, 3, 4, 5, 6] as d (d)}
            <span class="cell" class:now={w === 1 && d === 3}></span>
          {/each}
          {#each monthBars.filter((b) => b[0] === w) as b, i (i)}
            <span class="mbar c{b[3]}" style={`grid-column:${b[1] + 1} / span ${b[2]}`}></span>
          {/each}
        </div>
      {/each}
    </div>
  {:else if view === 'week' || view === 'day'}
    <div class="grid" class:day={view === 'day'}>
      <div class="gutter">
        {#each [0, 1, 2, 3] as h (h)}<i></i>{/each}
      </div>
      {#if view === 'week'}
        {#each [0, 1, 2, 3, 4] as d (d)}
          <div class="col" class:now={d === 2}>
            {#each weekEvents.filter((e) => e[0] === d) as e, i (i)}
              <span class="ev c{e[3]}" style={`top:${e[1] * 12.5}%;height:${e[2] * 12.5}%`}></span>
            {/each}
          </div>
        {/each}
      {:else}
        <div class="col now">
          {#each dayEvents as e, i (i)}
            <span class="ev wide c{e[2]}" style={`top:${e[0] * 12.5}%;height:${e[1] * 12.5}%`}></span>
          {/each}
        </div>
      {/if}
    </div>
  {:else}
    <div class="agenda">
      {#each [[1, 2], [3, 1], [4, 3]] as group, gi (gi)}
        <div class="group">
          <span class="date"></span>
          <div class="rows">
            {#each Array.from({ length: group[1] }, (_, i) => i) as r (r)}
              <span class="row"><i class="c{((gi + r) % 4) + 1}"></i><span class="line"></span></span>
            {/each}
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .stage {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 5px;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-raised);
    overflow: hidden;
  }

  .c1 {
    --c: var(--cal-1);
  }

  .c2 {
    --c: var(--cal-2);
  }

  .c3 {
    --c: var(--cal-4);
  }

  .c4 {
    --c: var(--cal-8);
  }

  .bar {
    display: flex;
    align-items: center;
    gap: 2px;
    height: 7px;
    flex-shrink: 0;
  }

  .bar i {
    width: 7px;
    height: 5px;
    border-radius: 1.5px;
    background: var(--border-default);
  }

  .bar i.today {
    width: 12px;
  }

  .title {
    width: 26%;
    height: 4px;
    margin-inline-start: 3px;
    border-radius: 1.5px;
    background: var(--text-tertiary);
    opacity: 0.6;
  }

  .month {
    display: grid;
    grid-template-rows: repeat(4, 1fr);
    flex: 1;
    min-height: 0;
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .week {
    display: grid;
    grid-template-columns: repeat(7, 1fr);
    grid-template-rows: 1fr;
    align-items: end;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .cell {
    grid-row: 1;
    align-self: stretch;
    border-inline-start: var(--hairline) solid var(--border-subtle);
  }

  .cell.now {
    background: var(--selection-bg);
  }

  .mbar {
    grid-row: 1;
    height: 4px;
    margin: 0 1px 3px;
    border-radius: 1.5px;
    background: color-mix(in srgb, var(--c) 45%, var(--surface-raised));
  }

  .grid {
    display: flex;
    flex: 1;
    min-height: 0;
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .gutter {
    display: flex;
    flex-direction: column;
    justify-content: space-around;
    width: 9px;
  }

  .gutter i {
    width: 5px;
    height: 2px;
    border-radius: 1px;
    background: var(--border-strong);
  }

  .col {
    position: relative;
    flex: 1;
    border-inline-start: var(--hairline) solid var(--border-subtle);
  }

  .col.now {
    background: color-mix(in srgb, var(--accent) 5%, transparent);
  }

  .ev {
    position: absolute;
    inset-inline: 1px;
    border-inline-start: 1.5px solid var(--c);
    border-radius: 1.5px;
    background: color-mix(in srgb, var(--c) 30%, var(--surface-raised));
  }

  .ev.wide {
    inset-inline: 2px 20%;
  }

  .agenda {
    display: flex;
    flex-direction: column;
    gap: 4px;
    flex: 1;
    min-height: 0;
    padding-top: 2px;
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .group {
    display: flex;
    gap: 6px;
  }

  .date {
    width: 14%;
    height: 5px;
    margin-top: 1px;
    border-radius: 1.5px;
    background: var(--text-tertiary);
    opacity: 0.6;
  }

  .rows {
    display: flex;
    flex-direction: column;
    gap: 3px;
    flex: 1;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .row i {
    width: 2px;
    height: 7px;
    border-radius: 1px;
    background: var(--c);
  }

  .line {
    flex: 1;
    max-width: 70%;
    height: 4px;
    border-radius: 1.5px;
    background: var(--border-default);
  }
</style>
