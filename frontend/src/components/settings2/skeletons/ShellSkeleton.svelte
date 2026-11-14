<script lang="ts">
  // a small picture of the app window, drawn from the theme tokens, that shows
  // one arrangement of it: which design, how the panes are laid out and drawn,
  // and where the space switcher sits. choice cards use it so a layout can be
  // picked by what it looks like.
  import type { DesignPref, LayoutMode, NavStyle, RailSide, PaneStyle } from '../../../lib/types'

  export let design: DesignPref = 'new'
  export let layout: LayoutMode = 'columns'
  export let nav: NavStyle = 'rail'
  export let railSide: RailSide = 'left'
  export let panes: PaneStyle = 'hybrid'
  /** Height of the picture in px. */
  export let height: number = 96

  $: classic = design === 'classic'
  $: rail = !classic && nav === 'rail'
</script>

<div
  class="window {classic ? 'classic' : panes}"
  class:rail-right={railSide === 'right'}
  style={`height:${height}px`}
  aria-hidden="true"
>
  {#if !classic}
    <div class="top">
      {#if nav === 'tabs'}
        <span class="tabs"><i class="on"></i><i></i><i></i></span>
      {/if}
      <span class="search"></span>
    </div>
  {/if}
  <div class="body">
    {#if rail}
      <div class="rail"><i class="on"></i><i></i><i></i><b></b></div>
    {/if}
    <div class="side">
      <span class="bar w70"></span>
      <span class="bar w50"></span>
      <span class="bar w60"></span>
      <span class="bar w40"></span>
      {#if !classic && nav === 'footer'}
        <span class="foot"><i class="on"></i><i></i><i></i></span>
      {/if}
    </div>
    <div class="surface" class:stacked={!classic && layout === 'stacked'}>
      <div class="list">
        {#each [0, 1, 2] as i (i)}
          <span class="item" class:on={i === 1}><span class="bar w80"></span><span class="bar w50"></span></span>
        {/each}
      </div>
      <div class="read">
        <span class="bar w60 strong"></span>
        <span class="bar w90"></span>
        <span class="bar w80"></span>
        <span class="bar w40"></span>
      </div>
    </div>
  </div>
</div>

<style>
  .window {
    --gap: 3px;
    display: flex;
    flex-direction: column;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-sunken);
    overflow: hidden;
    transition: background-color var(--motion-base) var(--ease-out);
  }

  .window.classic {
    background: var(--surface-base);
  }

  .top {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
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

  .tabs,
  .foot {
    display: flex;
    gap: 2px;
  }

  .tabs i,
  .foot i,
  .rail i {
    display: block;
    width: 5px;
    height: 5px;
    border-radius: 1.5px;
    background: var(--border-strong);
  }

  .tabs i.on,
  .foot i.on,
  .rail i.on {
    background: var(--accent);
  }

  .body {
    display: flex;
    flex: 1;
    min-height: 0;
    gap: 0;
  }

  .rail-right .body {
    flex-direction: row-reverse;
  }

  .rail {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 3px;
    width: 11px;
    padding-top: 2px;
    flex-shrink: 0;
  }

  .rail b {
    margin-top: auto;
    margin-bottom: 3px;
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--border-strong);
  }

  .side {
    display: flex;
    flex-direction: column;
    gap: 4px;
    width: 24%;
    padding: 5px;
    flex-shrink: 0;
  }

  .foot {
    margin-top: auto;
  }

  .surface {
    display: flex;
    flex: 1;
    min-width: 0;
    background: var(--surface-raised);
  }

  .surface.stacked {
    flex-direction: column;
  }

  .list {
    display: flex;
    flex-direction: column;
    width: 40%;
    border-inline-end: var(--hairline) solid var(--border-default);
  }

  .stacked .list {
    width: auto;
    height: 46%;
    border-inline-end: none;
    border-bottom: var(--hairline) solid var(--border-default);
    overflow: hidden;
  }

  .item {
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding: 4px 5px;
  }

  .stacked .item {
    flex-direction: row;
    padding: 3px 5px;
  }

  .item.on {
    background: var(--selection-bg-strong);
  }

  .read {
    display: flex;
    flex-direction: column;
    gap: 4px;
    flex: 1;
    padding: 6px;
    min-width: 0;
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
  .w50 { width: 50%; }
  .w60 { width: 60%; }
  .w70 { width: 70%; }
  .w80 { width: 80%; }
  .w90 { width: 90%; }

  /* classic: edge to edge, the sidebar split off by a hairline. */
  .classic .side {
    border-inline-end: var(--hairline) solid var(--border-default);
  }

  /* hybrid: list and reader share one raised, rounded surface. */
  .hybrid .surface {
    margin: 0 var(--gap) var(--gap) 0;
    border: var(--hairline) solid var(--border-default);
    border-radius: 3px;
    overflow: hidden;
  }

  /* flat: hairlines only. */
  .flat .surface {
    border-top: var(--hairline) solid var(--border-default);
    border-inline-start: var(--hairline) solid var(--border-default);
  }

  /* soft: every pane floats on its own. */
  .soft .body {
    gap: var(--gap);
    padding: 0 var(--gap) var(--gap);
  }

  .soft .side,
  .soft .list,
  .soft .read {
    border: var(--hairline) solid var(--border-default);
    border-radius: 3px;
    background: var(--surface-raised);
  }

  .soft .surface {
    gap: var(--gap);
    background: transparent;
  }
</style>
