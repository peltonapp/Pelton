<script lang="ts">
  // the top of the app window. with the native title bar the system draws a bar
  // of its own with the window buttons and the name, above Pelton's toolbar;
  // integrated, the buttons sit in that toolbar and the two bars are one.
  import Frame from './Frame.svelte'
  import MiniWindow from './MiniWindow.svelte'

  /** Uses the system's own title bar. */
  export let native: boolean = false
</script>

<Frame sunken>
  <div class="window" class:native>
    <div class="system">
      <span class="lights"><i></i><i></i><i></i></span>
      <span class="title">Pelton</span>
    </div>
    <div class="toolbar">
      <span class="lights"><i></i><i></i><i></i></span>
      <span class="tabs"><i class="on"></i><i></i></span>
      <span class="search"></span>
    </div>
    <div class="body"><MiniWindow /></div>
  </div>
</Frame>

<style>
  .window {
    display: flex;
    flex-direction: column;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-base);
    overflow: hidden;
  }

  .system,
  .toolbar {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    height: calc(var(--fz-body) + var(--space-4));
    padding: 0 var(--space-3);
    flex-shrink: 0;
  }

  .system {
    border-bottom: var(--hairline) solid var(--border-default);
    background: var(--surface-hover);
  }

  .toolbar {
    border-bottom: var(--hairline) solid var(--border-default);
  }

  /* integrated: the system bar is gone and the toolbar carries the buttons. */
  .window:not(.native) .system {
    display: none;
  }

  .window.native .toolbar .lights {
    display: none;
  }

  .lights {
    display: inline-flex;
    gap: var(--space-2);
  }

  .lights i {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--border-strong);
  }

  .title {
    flex: 1;
    text-align: center;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .tabs {
    display: inline-flex;
    gap: var(--space-2);
  }

  .tabs i {
    width: 26px;
    height: 8px;
    border-radius: var(--radius-control);
    background: var(--border-default);
  }

  .tabs i.on {
    background: var(--border-strong);
  }

  .search {
    flex: 1;
    max-width: 120px;
    height: 10px;
    margin-inline: auto;
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }

  .body {
    height: 56px;
  }
</style>
