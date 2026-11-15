<script lang="ts">
  // the reading pane with no message open: the chosen image as a small mark in
  // the middle, or filling the whole pane.
  import Frame from './Frame.svelte'

  /** The image the pane shows. */
  export let src: string
  /** Fills the pane with the image instead of a small mark. */
  export let fullscreen: boolean = false
</script>

<Frame>
  <div class="window">
    <div class="list">
      <span class="bar w80"></span>
      <span class="bar w50"></span>
      <span class="bar w80"></span>
    </div>
    <div class="pane">
      <img class:fill={fullscreen} {src} alt="" draggable="false" />
    </div>
  </div>
</Frame>

<style>
  .window {
    display: grid;
    grid-template-columns: 1fr 2fr;
    height: 96px;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    overflow: hidden;
  }

  .list {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-3);
    border-inline-end: var(--hairline) solid var(--border-default);
    background: var(--surface-raised);
  }

  .pane {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--surface-sunken);
    overflow: hidden;
  }

  img {
    width: 32px;
    height: 32px;
    object-fit: contain;
    transition:
      width var(--motion-slow) var(--ease-out),
      height var(--motion-slow) var(--ease-out);
  }

  img.fill {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }

  .bar {
    display: block;
    height: 3px;
    border-radius: 1.5px;
    background: var(--border-strong);
  }

  .w50 { width: 50%; }
  .w80 { width: 80%; }
</style>
