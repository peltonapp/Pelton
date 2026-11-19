<script lang="ts">
  // a small picture of what the reader does once the open message is archived:
  // three list rows, the middle one leaving, and the band landing on the row
  // above (newer), on the row below (older), or on none (back to the list).
  import type { AfterMove } from '../../../lib/types'

  /** The choice to draw. */
  export let mode: AfterMove = 'older'

  $: target = mode === 'newer' ? 0 : mode === 'older' ? 2 : -1
</script>

<div class="stage" aria-hidden="true">
  <div class="list">
    {#each [0, 1, 2] as row (row)}
      <div class="row" class:leaving={row === 1} class:target={row === target}>
        <span class="bar w40 strong"></span>
        <span class="bar w75"></span>
      </div>
    {/each}
  </div>
  <div class="reader" class:empty={target < 0}>
    {#if target >= 0}
      <span class="bar w55 strong"></span>
      <span class="bar w90"></span>
      <span class="bar w80"></span>
      <span class="bar w60"></span>
    {/if}
  </div>
</div>

<style>
  .stage {
    display: flex;
    width: 220px;
    height: 96px;
    overflow: hidden;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-raised);
  }

  .list {
    display: flex;
    flex-direction: column;
    width: 42%;
    border-inline-end: var(--hairline) solid var(--border-default);
  }

  .row {
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: 4px;
    flex: 1;
    padding: 0 8px;
    border-bottom: var(--hairline) solid var(--border-subtle);
    transition: background-color var(--motion-base) var(--ease-out);
  }

  .row.leaving {
    opacity: 0.3;
    outline: var(--hairline) dashed var(--border-strong);
    outline-offset: -3px;
  }

  .row.target {
    background: var(--selection-bg);
    box-shadow: inset 2px 0 0 var(--accent);
  }

  .reader {
    display: flex;
    flex-direction: column;
    gap: 5px;
    flex: 1;
    padding: 10px;
  }

  .reader.empty {
    background: var(--surface-sunken);
  }

  .bar {
    display: block;
    height: 3px;
    border-radius: 1.5px;
    background: var(--border-strong);
  }

  .bar.strong {
    height: 4px;
    background: var(--text-tertiary);
  }

  .w40 { width: 40%; }
  .w55 { width: 55%; }
  .w60 { width: 60%; }
  .w75 { width: 75%; }
  .w80 { width: 80%; }
  .w90 { width: 90%; }
</style>
