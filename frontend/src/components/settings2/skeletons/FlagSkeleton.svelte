<script lang="ts">
  // a small picture of two list rows, the first one flagged, that shows how a
  // flag is marked: by an icon, by a bar on the row's edge, by both or not at all.
  import flagIcon from '@phosphor-icons/core/assets/duotone/flag-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'

  /** How a flagged message is marked. */
  export let mode: 'flag' | 'left' | 'both' | 'off' = 'flag'
  /** Height of the picture in px. */
  export let height: number = 56

  $: bar = mode === 'left' || mode === 'both'
  $: icon = mode === 'flag' || mode === 'both'
</script>

<div class="rows" style={`height:${height}px`} aria-hidden="true">
  {#each [0, 1] as i (i)}
    <div class="row" class:bar={i === 0 && bar}>
      <span class="lines">
        <span class="line">
          <span class="bar-line w45 strong"></span>
          {#if i === 0 && icon}
            <span class="flag"><Ph svg={flagIcon} size={10} /></span>
          {/if}
          <span class="bar-line time" class:after={!(i === 0 && icon)}></span>
        </span>
        <span class="bar-line w80"></span>
      </span>
    </div>
  {/each}
</div>

<style>
  .rows {
    display: flex;
    flex-direction: column;
    justify-content: center;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-raised);
    overflow: hidden;
  }

  .row {
    position: relative;
    display: flex;
    align-items: center;
    flex: 1;
    padding: 0 8px;
  }

  .row + .row {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .row.bar::before {
    content: '';
    position: absolute;
    top: 0;
    bottom: 0;
    inset-inline-start: 0;
    width: 3px;
    background: var(--warning);
  }

  .lines {
    display: flex;
    flex-direction: column;
    gap: 4px;
    flex: 1;
    min-width: 0;
  }

  .line {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .flag {
    display: inline-flex;
    margin-inline-start: auto;
    color: var(--warning);
  }

  .bar-line {
    display: block;
    height: 3px;
    border-radius: 1.5px;
    background: var(--border-strong);
  }

  .bar-line.strong {
    height: 4px;
    background: var(--text-tertiary);
  }

  .bar-line.time {
    width: 12%;
  }

  .bar-line.time.after {
    margin-inline-start: auto;
  }

  .w45 { width: 45%; }
  .w80 { width: 80%; }
</style>
