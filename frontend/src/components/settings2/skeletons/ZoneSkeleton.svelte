<script lang="ts">
  // a small picture of the time grid's hour gutter: plain hours, hours under
  // the zone's name, or a second column of hours in another zone.
  import type { TimeZoneDisplay } from '../../../lib/types'

  /** The display to draw. */
  export let mode: TimeZoneDisplay = 'off'
  /** Height of the picture in px. */
  export let height: number = 72
</script>

<div class="stage" style={`height:${height}px`} aria-hidden="true">
  <div class="gutters">
    {#if mode === 'dual'}
      <div class="gutter alt">
        <span class="zone"></span>
        {#each [0, 1, 2, 3] as h (h)}<i></i>{/each}
      </div>
    {/if}
    <div class="gutter">
      {#if mode !== 'off'}<span class="zone"></span>{:else}<span class="zone blank"></span>{/if}
      {#each [0, 1, 2, 3] as h (h)}<i></i>{/each}
    </div>
  </div>
  <div class="cols">
    {#each [0, 1, 2] as c (c)}
      <div class="col">
        {#if c === 1}<span class="ev"></span>{/if}
      </div>
    {/each}
  </div>
</div>

<style>
  .stage {
    display: flex;
    padding: 5px;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-raised);
    overflow: hidden;
  }

  .gutters {
    display: flex;
    gap: 3px;
    padding-inline-end: 3px;
  }

  .gutter {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    justify-content: space-between;
    gap: 3px;
    width: 14px;
  }

  .gutter i {
    width: 9px;
    height: 3px;
    border-radius: 1px;
    background: var(--border-strong);
  }

  .alt i {
    background: var(--border-default);
  }

  .zone {
    width: 12px;
    height: 4px;
    border-radius: 1px;
    background: var(--accent);
    opacity: 0.8;
  }

  .zone.blank {
    background: transparent;
  }

  .cols {
    display: flex;
    flex: 1;
    border-top: var(--hairline) solid var(--border-subtle);
    margin-top: 7px;
  }

  .col {
    position: relative;
    flex: 1;
    border-inline-start: var(--hairline) solid var(--border-subtle);
  }

  .ev {
    position: absolute;
    top: 25%;
    height: 35%;
    inset-inline: 1px;
    border-inline-start: 1.5px solid var(--cal-2);
    border-radius: 1.5px;
    background: color-mix(in srgb, var(--cal-2) 30%, var(--surface-raised));
  }
</style>
