<script lang="ts">
  // a small picture of two message list rows, drawn from the theme tokens, that
  // shows where the sender picture goes.
  import type { AvatarPlacement } from '../../../lib/types'

  export let avatar: AvatarPlacement = 'inline'
  /** Height of the picture in px. */
  export let height: number = 72
</script>

<div class="rows" style={`height:${height}px`} aria-hidden="true">
  {#each [0, 1] as i (i)}
    <div class="row" class:on={i === 0}>
      {#if avatar === 'column'}
        <span class="pic big"></span>
      {/if}
      <span class="lines">
        <span class="line">
          {#if avatar === 'inline'}
            <span class="pic"></span>
          {/if}
          <span class="bar w45 strong"></span>
          <span class="bar time"></span>
        </span>
        <span class="bar w80"></span>
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
    display: flex;
    align-items: center;
    gap: 6px;
    flex: 1;
    padding: 0 8px;
  }

  .row.on {
    background: var(--selection-bg);
  }

  .row + .row {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .lines {
    display: flex;
    flex-direction: column;
    gap: 5px;
    flex: 1;
    min-width: 0;
  }

  .line {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .pic {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    opacity: 0.7;
    flex-shrink: 0;
  }

  .pic.big {
    width: 16px;
    height: 16px;
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

  .bar.time {
    width: 12%;
    margin-inline-start: auto;
  }

  .w45 { width: 45%; }
  .w80 { width: 80%; }
</style>
