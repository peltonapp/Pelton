<script lang="ts">
  // the script mark: the Automations lightning, drawn by the app beside every
  // button and menu entry that runs a script, so a script cannot pass for one
  // of the app's own actions. inline it sits where an icon would; as a badge
  // it rides the corner of the button it is in, which needs a positioned
  // parent.
  import lightning from '@phosphor-icons/core/assets/duotone/lightning-duotone.svg?raw'
  import Ph from './Ph.svelte'

  /** Draws the mark on the corner of its button instead of inline. */
  export let badge = false
  /** Edge length in px. */
  export let size = badge ? 9 : 14
</script>

<span class="script-mark" class:badge data-script-mark=""><Ph svg={lightning} {size} /></span>

<style>
  .script-mark {
    display: inline-flex;
    flex-shrink: 0;
    color: var(--accent);
  }

  .badge {
    position: absolute;
    inset-block-end: 0;
    inset-inline-end: 0;
    padding: 1px;
    border-radius: 999px;
    background: var(--surface-overlay);
    pointer-events: none;
  }
</style>
