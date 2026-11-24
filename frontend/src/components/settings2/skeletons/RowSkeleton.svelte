<script lang="ts">
  // a small picture of message list rows, drawn from the theme tokens, that
  // shows where the sender picture goes and how tall a row template is.
  import type { AvatarPlacement, RowTemplate } from '../../../lib/types'

  /** Where the sender picture sits. */
  export let avatar: AvatarPlacement = 'inline'
  /** Row template; sets the number of lines per row and how many rows fit. */
  export let template: RowTemplate = 'compact'
  /** Height of the picture in px. */
  export let height: number = 72

  $: count = template === 'single' ? 4 : template === 'compact' ? 3 : 2
  $: snippet = template === 'relaxed' || template === 'comfortable'
  $: column = avatar === 'column' && template !== 'single'
</script>

<div class="rows {template}" style={`height:${height}px`} aria-hidden="true">
  {#each Array(count) as _, i (i)}
    <div class="row" class:on={i === 0}>
      {#if column}
        <span class="pic big"></span>
      {/if}
      <span class="lines">
        <span class="line">
          {#if avatar === 'inline'}
            <span class="pic"></span>
          {/if}
          <span class="bar w45 strong"></span>
          {#if template === 'single'}
            <span class="bar w30"></span>
          {/if}
          <span class="bar time"></span>
        </span>
        {#if template !== 'single'}
          <span class="bar w80"></span>
        {/if}
        {#if snippet}
          <span class="bar w60 faint"></span>
        {/if}
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

  .relaxed .lines {
    gap: 4px;
  }

  .comfortable .lines,
  .compact .lines {
    gap: 3px;
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

  .bar.faint {
    opacity: 0.5;
  }

  .bar.time {
    width: 12%;
    margin-inline-start: auto;
  }

  .w30 { width: 30%; }
  .w45 { width: 45%; }
  .w60 { width: 60%; }
  .w80 { width: 80%; }
</style>
