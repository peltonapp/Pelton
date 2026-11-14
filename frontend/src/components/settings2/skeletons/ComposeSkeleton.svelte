<script lang="ts">
  // a small picture, drawn from the theme tokens, of where a new message
  // opens: a window of its own beside the app, a panel docked in the corner,
  // or a tab filling the content area. choice cards use it so the compose
  // placement can be picked by what it looks like.
  import type { ComposeOpenIn } from '../../../lib/types'

  /** The placement to draw. */
  export let mode: ComposeOpenIn = 'window'
  /** Height of the picture in px. */
  export let height: number = 96
</script>

<div class="stage {mode}" style={`height:${height}px`} aria-hidden="true">
  <div class="window">
    <div class="top"><span class="search"></span></div>
    <div class="body">
      <div class="side">
        <span class="bar w70"></span>
        <span class="bar w50"></span>
        <span class="bar w60"></span>
      </div>
      <div class="surface">
        {#if mode === 'tab'}
          <div class="tabs"><i></i><i class="on"></i></div>
          <div class="composer full">
            <span class="send"></span>
            <span class="bar w60"></span>
            <span class="bar w80 strong"></span>
            <span class="bar w90"></span>
            <span class="bar w70"></span>
          </div>
        {:else}
          <div class="list">
            {#each [0, 1, 2] as i (i)}
              <span class="item"><span class="bar w80"></span><span class="bar w50"></span></span>
            {/each}
          </div>
          <div class="read">
            <span class="bar w60 strong"></span>
            <span class="bar w90"></span>
            <span class="bar w80"></span>
          </div>
        {/if}
      </div>
    </div>
    {#if mode === 'docked'}
      <div class="composer docked">
        <span class="bar w50 strong"></span>
        <span class="bar w90"></span>
        <span class="bar w70"></span>
        <span class="send"></span>
      </div>
    {/if}
  </div>
  {#if mode === 'window'}
    <div class="composer floating">
      <span class="send"></span>
      <span class="bar w60"></span>
      <span class="bar w80 strong"></span>
      <span class="bar w90"></span>
    </div>
  {/if}
</div>

<style>
  .stage {
    position: relative;
    display: flex;
    align-items: stretch;
  }

  .window {
    --gap: 3px;
    position: relative;
    display: flex;
    flex-direction: column;
    flex: 1;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-sunken);
    overflow: hidden;
  }

  .stage.window .window {
    margin-inline-end: 22%;
    opacity: 0.75;
  }

  .top {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 11px;
    flex-shrink: 0;
  }

  .search {
    width: 34%;
    height: 5px;
    border-radius: 2px;
    background: var(--surface-raised);
    box-shadow: 0 0 0 var(--hairline) var(--border-default);
  }

  .body {
    display: flex;
    flex: 1;
    gap: var(--gap);
    padding: 0 var(--gap) var(--gap);
    min-height: 0;
  }

  .side {
    display: flex;
    flex-direction: column;
    gap: 4px;
    width: 22%;
    padding: 4px 3px;
  }

  .surface {
    position: relative;
    display: flex;
    flex: 1;
    min-width: 0;
    border: var(--hairline) solid var(--border-default);
    border-radius: 3px;
    background: var(--surface-raised);
    overflow: hidden;
  }

  .list {
    display: flex;
    flex-direction: column;
    width: 40%;
    border-inline-end: var(--hairline) solid var(--border-subtle);
  }

  .item {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 3px 4px;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .read {
    display: flex;
    flex-direction: column;
    gap: 4px;
    flex: 1;
    padding: 5px;
  }

  .tabs {
    position: absolute;
    inset: 0 0 auto 0;
    display: flex;
    gap: 2px;
    height: 8px;
    padding: 2px 3px;
    background: var(--surface-sunken);
  }

  .tabs i {
    display: block;
    width: 16px;
    border-radius: 1.5px;
    background: var(--border-default);
  }

  .tabs i.on {
    background: var(--surface-raised);
    box-shadow: inset 0 -1.5px 0 var(--accent);
  }

  .composer {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 5px;
    background: var(--surface-raised);
  }

  .composer.full {
    flex: 1;
    margin-top: 8px;
  }

  .composer.docked {
    position: absolute;
    inset-inline-end: 6px;
    bottom: 5px;
    width: 42%;
    height: 54%;
    border: var(--hairline) solid var(--border-strong);
    border-radius: 3px;
    box-shadow: var(--shadow-overlay);
  }

  .composer.floating {
    position: absolute;
    inset-inline-end: 0;
    top: 14%;
    width: 46%;
    height: 80%;
    border: var(--hairline) solid var(--border-strong);
    border-radius: calc(var(--radius-card) - 2px);
    box-shadow: var(--shadow-overlay);
  }

  .send {
    display: block;
    width: 26%;
    height: 5px;
    border-radius: 1.5px;
    background: var(--accent);
  }

  .composer.docked .send {
    margin-top: auto;
  }

  .bar {
    display: block;
    height: 3px;
    border-radius: 1.5px;
    background: var(--border-default);
  }

  .bar.strong {
    background: var(--border-strong);
  }

  .w50 {
    width: 50%;
  }
  .w60 {
    width: 60%;
  }
  .w70 {
    width: 70%;
  }
  .w80 {
    width: 80%;
  }
  .w90 {
    width: 90%;
  }
</style>
