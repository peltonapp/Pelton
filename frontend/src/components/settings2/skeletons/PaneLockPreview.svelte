<script lang="ts">
  // the dividers between the panes. unlocked, each one carries a drag handle;
  // locked, a padlock sits there and the panes keep their size.
  import lockIcon from '@phosphor-icons/core/assets/duotone/lock-simple-duotone.svg?raw'
  import dragIcon from '@phosphor-icons/core/assets/duotone/arrows-left-right-duotone.svg?raw'
  import Frame from './Frame.svelte'
  import Ph from '../../common/Ph.svelte'

  /** Draws the panes locked. */
  export let locked: boolean = false
</script>

<Frame sunken>
  <div class="panes" class:locked>
    <div class="pane side">
      <span class="bar w70"></span>
      <span class="bar w50"></span>
    </div>
    <span class="divider"><span class="grip"><Ph svg={locked ? lockIcon : dragIcon} size={14} /></span></span>
    <div class="pane list">
      <span class="bar w80"></span>
      <span class="bar w50"></span>
      <span class="bar w80"></span>
    </div>
    <span class="divider"><span class="grip"><Ph svg={locked ? lockIcon : dragIcon} size={14} /></span></span>
    <div class="pane read">
      <span class="bar w60 strong"></span>
      <span class="bar w90"></span>
      <span class="bar w80"></span>
    </div>
  </div>
</Frame>

<style>
  .panes {
    display: grid;
    grid-template-columns: 1fr 1px 1.4fr 1px 2fr;
    height: 72px;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-raised);
    overflow: hidden;
  }

  .pane {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-3);
  }

  .pane.side {
    background: var(--surface-sunken);
  }

  .divider {
    position: relative;
    background: var(--border-default);
  }

  .grip {
    position: absolute;
    top: 50%;
    inset-inline-start: 50%;
    display: inline-flex;
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    color: var(--accent);
    transform: translate(-50%, -50%);
  }

  .locked .grip {
    color: var(--text-tertiary);
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

  .w50 { width: 50%; }
  .w60 { width: 60%; }
  .w70 { width: 70%; }
  .w80 { width: 80%; }
  .w90 { width: 90%; }
</style>
