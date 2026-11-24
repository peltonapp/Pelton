<script lang="ts">
  // a looping picture of interface motion: a dot slides along a track while a
  // row fades in and out. with `still` set the loop stops and the picture
  // holds one frame.

  /** Stops the animation. */
  export let still: boolean = false
</script>

<div class="stage" class:still aria-hidden="true">
  <div class="track"><span class="dot"></span></div>
  <div class="rows">
    <span class="row"><span class="bar w45 strong"></span><span class="bar w80"></span></span>
    <span class="row fade"><span class="bar w60 strong"></span><span class="bar w70"></span></span>
  </div>
</div>

<style>
  .stage {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    width: 100%;
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-raised);
    box-sizing: border-box;
  }

  .track {
    position: relative;
    height: 12px;
    border-radius: 6px;
    background: var(--surface-sunken);
  }

  .dot {
    position: absolute;
    inset-block: 0;
    inset-inline-start: 0;
    width: 12px;
    border-radius: 50%;
    background: var(--accent);
    animation: slide calc(var(--motion-slow) * 8) var(--ease-in-out) infinite alternate;
  }

  .rows {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .row {
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
  }

  .row.fade {
    animation: fade calc(var(--motion-slow) * 8) var(--ease-in-out) infinite alternate;
  }

  .stage.still .dot,
  .stage.still .row.fade {
    animation: none;
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
  .w60 { width: 60%; }
  .w70 { width: 70%; }
  .w80 { width: 80%; }

  @keyframes slide {
    to {
      inset-inline-start: calc(100% - 12px);
    }
  }

  @keyframes fade {
    from {
      opacity: 0.15;
    }
    to {
      opacity: 1;
    }
  }
</style>
