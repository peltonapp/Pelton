<script lang="ts">
  // a picture of the contacts space in one of its two views, drawn from the
  // theme tokens: the book pane, then either a list with a contact beside it or
  // a grid of cards.
  import type { ContactsView } from '../../../lib/types'

  /** Which view to draw. */
  export let view: ContactsView = 'panes'
  /** Height of the picture in px. */
  export let height: number = 96
</script>

<div class="window" style={`height:${height}px`} aria-hidden="true">
  <div class="side">
    <span class="bar on w70"></span>
    <span class="bar w50"></span>
    <span class="bar w60"></span>
  </div>
  {#if view === 'panes'}
    <div class="list">
      {#each [0, 1, 2, 3] as i (i)}
        <span class="item" class:on={i === 1}>
          <span class="pic"></span>
          <span class="lines"><span class="bar w80"></span><span class="bar w50"></span></span>
        </span>
      {/each}
    </div>
    <div class="detail">
      <span class="pic big"></span>
      <span class="bar strong w60"></span>
      <span class="bar w80"></span>
      <span class="bar w50"></span>
    </div>
  {:else}
    <div class="grid">
      {#each [0, 1, 2, 3, 4, 5] as i (i)}
        <span class="card" class:on={i === 1}>
          <span class="pic"></span>
          <span class="bar w80"></span>
          <span class="bar w50"></span>
        </span>
      {/each}
    </div>
  {/if}
</div>

<style>
  .window {
    display: flex;
    width: 100%;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-raised);
    overflow: hidden;
  }

  .side {
    display: flex;
    flex-direction: column;
    gap: 4px;
    width: 22%;
    padding: 6px;
    flex-shrink: 0;
    background: var(--surface-sunken);
    border-inline-end: var(--hairline) solid var(--border-default);
  }

  .list {
    display: flex;
    flex-direction: column;
    width: 38%;
    flex-shrink: 0;
    border-inline-end: var(--hairline) solid var(--border-default);
  }

  .item {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 4px 5px;
  }

  .item.on {
    background: var(--selection-bg-strong);
  }

  .lines {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .detail {
    display: flex;
    flex: 1;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
    padding: 7px;
  }

  .grid {
    display: grid;
    flex: 1;
    grid-template-columns: repeat(3, 1fr);
    grid-auto-rows: min-content;
    align-content: start;
    gap: 4px;
    padding: 6px;
  }

  .card {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 4px;
    border: var(--hairline) solid var(--border-default);
    border-radius: 2px;
  }

  .card.on {
    border-color: var(--accent);
    background: var(--selection-bg);
  }

  .pic {
    flex-shrink: 0;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--border-strong);
  }

  .pic.big {
    width: 14px;
    height: 14px;
    margin-bottom: 2px;
  }

  .item.on .pic,
  .card.on .pic {
    background: var(--accent);
  }

  .bar {
    display: block;
    height: 3px;
    border-radius: 1.5px;
    background: var(--border-strong);
  }

  .bar.on {
    background: var(--accent);
  }

  .bar.strong {
    height: 4px;
    background: var(--text-tertiary);
  }

  .w50 { width: 50%; }
  .w60 { width: 60%; }
  .w70 { width: 70%; }
  .w80 { width: 80%; }
</style>
