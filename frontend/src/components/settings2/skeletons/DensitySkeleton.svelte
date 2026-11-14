<script lang="ts">
  // a picture of how tightly rows and controls are packed: the same list drawn
  // with the spacing of one density.
  import type { DensityPref } from '../../../lib/types'

  /** Which density to draw. */
  export let density: DensityPref = 'medium'
  /** Height of the picture in px. */
  export let height: number = 84

  const rows: Record<DensityPref, number> = { compact: 5, medium: 4, luxe: 3 }
  $: count = rows[density]
</script>

<div class="list" style={`height:${height}px`} aria-hidden="true">
  {#each Array.from({ length: count }, (_, i) => i) as i (i)}
    <span class="row" class:on={i === 1}>
      <span class="bar w45 strong"></span>
      {#if density !== 'compact'}
        <span class="bar w80"></span>
      {/if}
    </span>
  {/each}
</div>

<style>
  .list {
    display: flex;
    flex-direction: column;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-raised);
    overflow: hidden;
  }

  .row {
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: 3px;
    flex: 1;
    padding: 0 8px;
  }

  .row + .row {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .row.on {
    background: var(--selection-bg);
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

  .w45 { width: 45%; }
  .w80 { width: 80%; }
</style>
