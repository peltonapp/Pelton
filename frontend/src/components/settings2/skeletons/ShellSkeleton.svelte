<script lang="ts">
  // a picture of the app window, drawn from the theme tokens, that shows one
  // arrangement of it: which design, how the panes are laid out and drawn, and
  // where the space switcher sits. choice cards use the small size so a layout
  // can be picked by what it looks like; onboarding uses the large one as a live
  // preview. every part stays in the tree and moves by css, so a change of
  // props morphs the picture instead of redrawing it.
  import type { AvatarPlacement, DesignPref, LayoutMode, NavStyle, RailSide, PaneStyle } from '../../../lib/types'

  export let design: DesignPref = 'new'
  export let layout: LayoutMode = 'columns'
  export let nav: NavStyle = 'rail'
  export let railSide: RailSide = 'left'
  export let panes: PaneStyle = 'hybrid'
  /** Height of the small picture in px. The large one follows its width. */
  export let height: number = 96
  /** small fits a choice card; large fills the width of its container. */
  export let size: 'small' | 'large' = 'small'
  /** Where list rows draw the sender picture; null draws none. */
  export let avatar: AvatarPlacement | null = null

  $: classic = design === 'classic'
  $: rail = !classic && nav === 'rail'
  $: stacked = !classic && layout === 'stacked'
  $: rows = size === 'large' ? [0, 1, 2, 3, 4, 5, 6] : [0, 1, 2]
</script>

<div
  class="window {classic ? 'classic' : panes}"
  class:large={size === 'large'}
  class:stacked
  style={size === 'large' ? undefined : `height:${height}px`}
  aria-hidden="true"
>
  <div class="top" class:shown={!classic}>
    <span class="tabs" class:shown={!classic && nav === 'tabs'}><i class="on"></i><i></i><i></i></span>
    <span class="search"></span>
  </div>
  <div class="body">
    <div class="rail" class:shown={rail && railSide === 'left'}><i class="on"></i><i></i><i></i><b></b></div>
    <div class="side">
      <span class="bar w70"></span>
      <span class="bar w50"></span>
      <span class="bar w60"></span>
      <span class="bar w40"></span>
      <span class="foot" class:shown={!classic && nav === 'footer'}><i class="on"></i><i></i><i></i></span>
    </div>
    <div class="surface">
      <div class="list">
        {#each rows as i (i)}
          <span class="item" class:on={i === 1}>
            {#if avatar === 'column'}
              <span class="pic"></span>
            {/if}
            <span class="lines">
              <span class="line">
                {#if avatar === 'inline'}
                  <span class="pic inline"></span>
                {/if}
                <span class="bar w80" class:strong={size === 'large' && i < 2}></span>
              </span>
              <span class="bar w50"></span>
            </span>
          </span>
        {/each}
      </div>
      <div class="read">
        <span class="bar w60 strong"></span>
        <span class="bar w90"></span>
        <span class="bar w80"></span>
        <span class="bar w40"></span>
        {#if size === 'large'}
          <span class="gap"></span>
          <span class="bar w90"></span>
          <span class="bar w70"></span>
          <span class="bar w80"></span>
        {/if}
      </div>
    </div>
    <div class="rail" class:shown={rail && railSide === 'right'}><i class="on"></i><i></i><i></i><b></b></div>
  </div>
</div>

<style>
  /* --k scales every fixed size of the picture, so the large preview is the
     small one drawn bigger rather than a second drawing. */
  .window {
    --k: 1;
    --gap: calc(3px * var(--k));
    --morph: var(--motion-slow) var(--ease-out);
    display: flex;
    flex-direction: column;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-sunken);
    overflow: hidden;
    transition: background-color var(--morph);
  }

  .window.large {
    --k: 2.2;
    width: 100%;
    aspect-ratio: 16 / 10;
    border-radius: var(--radius-card);
  }

  .window.classic {
    background: var(--surface-base);
  }

  .top {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 0;
    opacity: 0;
    flex-shrink: 0;
    overflow: hidden;
    transition:
      height var(--morph),
      opacity var(--morph);
  }

  .top.shown {
    height: calc(11px * var(--k));
    opacity: 1;
  }

  .search {
    width: 34%;
    height: calc(5px * var(--k));
    border-radius: calc(2px * var(--k));
    background: var(--surface-raised);
    box-shadow: 0 0 0 var(--hairline) var(--border-default);
  }

  .tabs,
  .foot {
    display: flex;
    gap: calc(2px * var(--k));
    overflow: hidden;
  }

  .tabs {
    max-width: 0;
    opacity: 0;
    transition:
      max-width var(--morph),
      opacity var(--morph),
      margin var(--morph);
  }

  .tabs.shown {
    max-width: 30%;
    opacity: 1;
    margin-inline-end: calc(4px * var(--k));
  }

  .tabs i,
  .foot i,
  .rail i {
    display: block;
    flex-shrink: 0;
    width: calc(5px * var(--k));
    height: calc(5px * var(--k));
    border-radius: calc(1.5px * var(--k));
    background: var(--border-strong);
    transition: background-color var(--morph);
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
    transition:
      gap var(--morph),
      padding var(--morph);
  }

  .rail {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: calc(3px * var(--k));
    width: 0;
    padding-top: calc(2px * var(--k));
    flex-shrink: 0;
    overflow: hidden;
    opacity: 0;
    transition:
      width var(--morph),
      opacity var(--morph);
  }

  .rail.shown {
    width: calc(11px * var(--k));
    opacity: 1;
  }

  .rail b {
    margin-top: auto;
    margin-bottom: calc(3px * var(--k));
    width: calc(5px * var(--k));
    height: calc(5px * var(--k));
    flex-shrink: 0;
    border-radius: 50%;
    background: var(--border-strong);
  }

  .side {
    display: flex;
    flex-direction: column;
    gap: calc(4px * var(--k));
    width: 24%;
    padding: calc(5px * var(--k));
    flex-shrink: 0;
    border: var(--hairline) solid transparent;
    transition:
      border-color var(--morph),
      border-radius var(--morph),
      background-color var(--morph);
  }

  .foot {
    margin-top: auto;
    max-height: 0;
    opacity: 0;
    transition:
      max-height var(--morph),
      opacity var(--morph);
  }

  .foot.shown {
    max-height: calc(6px * var(--k));
    opacity: 1;
  }

  /* the list and the reader are placed by their edges, so moving between side
     by side and stacked slides the edges instead of jumping. --half opens the
     seam between them for the soft style. */
  .surface {
    --half: 0px;
    position: relative;
    flex: 1;
    min-width: 0;
    background: var(--surface-raised);
    border: var(--hairline) solid transparent;
    transition:
      margin var(--morph),
      border-color var(--morph),
      border-radius var(--morph),
      background-color var(--morph);
  }

  .list,
  .read {
    position: absolute;
    top: 0;
    bottom: 0;
    inset-inline-start: 0;
    inset-inline-end: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    border: var(--hairline) solid transparent;
    transition:
      top var(--morph),
      bottom var(--morph),
      inset-inline-start var(--morph),
      inset-inline-end var(--morph),
      border-color var(--morph),
      border-radius var(--morph),
      background-color var(--morph);
  }

  .list {
    inset-inline-end: calc(60% + var(--half));
    border-inline-end-color: var(--border-default);
  }

  .read {
    inset-inline-start: calc(40% + var(--half));
    gap: calc(4px * var(--k));
    padding: calc(6px * var(--k));
  }

  .stacked .list {
    inset-inline-end: 0;
    bottom: calc(54% + var(--half));
    border-inline-end-color: transparent;
    border-bottom-color: var(--border-default);
  }

  .stacked .read {
    inset-inline-start: 0;
    top: calc(46% + var(--half));
  }

  .item {
    display: flex;
    align-items: center;
    gap: calc(4px * var(--k));
    padding: calc(4px * var(--k)) calc(5px * var(--k));
    flex-shrink: 0;
    transition:
      padding var(--morph),
      background-color var(--morph);
  }

  /* the large preview spaces its rows with the density tokens, so the density
     choice shows in it. */
  .large .item {
    padding: calc(var(--row-pad-y) * 0.55) calc(5px * var(--k));
  }

  .item.on {
    background: var(--selection-bg-strong);
  }

  .lines {
    display: flex;
    flex-direction: column;
    gap: calc(3px * var(--k));
    flex: 1;
    min-width: 0;
  }

  .large .lines {
    gap: calc(var(--row-gap) * 1.2);
  }

  .stacked .lines {
    flex-direction: row;
    align-items: center;
  }

  .line {
    display: flex;
    align-items: center;
    gap: calc(2px * var(--k));
    min-width: 0;
  }

  .stacked .line {
    flex: 1;
  }

  .line .bar {
    flex: 1;
  }

  .pic {
    flex-shrink: 0;
    width: calc(8px * var(--k));
    height: calc(8px * var(--k));
    border-radius: 50%;
    background: var(--border-strong);
  }

  .pic.inline {
    width: calc(4px * var(--k));
    height: calc(4px * var(--k));
  }

  .item.on .pic {
    background: var(--accent);
  }

  .bar {
    display: block;
    height: calc(3px * var(--k));
    border-radius: calc(1.5px * var(--k));
    background: var(--border-strong);
    flex-shrink: 0;
  }

  .bar.strong {
    height: calc(4px * var(--k));
    background: var(--text-tertiary);
  }

  .gap {
    height: calc(4px * var(--k));
  }

  .w40 { width: 40%; }
  .w50 { width: 50%; }
  .w60 { width: 60%; }
  .w70 { width: 70%; }
  .w80 { width: 80%; }
  .w90 { width: 90%; }

  /* classic: edge to edge, the sidebar split off by a hairline. */
  .classic .side {
    border-inline-end-color: var(--border-default);
  }

  /* hybrid: list and reader share one raised, rounded surface. */
  .hybrid .surface {
    margin-inline-end: var(--gap);
    margin-bottom: var(--gap);
    border-color: var(--border-default);
    border-radius: calc(3px * var(--k));
    overflow: hidden;
  }

  /* flat: hairlines only. */
  .flat .surface {
    border-top-color: var(--border-default);
    border-inline-start-color: var(--border-default);
  }

  /* soft: every pane floats on its own. */
  .soft .body {
    gap: var(--gap);
    padding: 0 var(--gap) var(--gap);
  }

  .soft .surface {
    --half: calc(var(--gap) / 2);
    background: transparent;
  }

  .soft .side,
  .soft .list,
  .soft .read {
    border-color: var(--border-default);
    border-radius: calc(3px * var(--k));
    background: var(--surface-raised);
  }
</style>
