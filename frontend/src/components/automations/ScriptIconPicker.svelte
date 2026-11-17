<script lang="ts">
  // a grid of the icons a script can wear on a button, from the app's set.
  import { createEventDispatcher } from 'svelte'
  import Ph from '../common/Ph.svelte'
  import { defaultScriptIcon, scriptIcons } from '../../lib/scripticons'

  /** The picked icon's name. */
  export let value: unknown = defaultScriptIcon
  /** The group's accessible name. */
  export let label: string

  const dispatch = createEventDispatcher<{ change: string }>()
</script>

<div class="icons" role="radiogroup" aria-label={label}>
  {#each Object.entries(scriptIcons) as [name, svg] (name)}
    <button type="button" class="icon" class:on={value === name} role="radio" aria-checked={value === name} aria-label={name} title={name} on:click={() => dispatch('change', name)}>
      <Ph {svg} size={16} />
    </button>
  {/each}
</div>

<style>
  .icons {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }

  .icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--control-height);
    height: var(--control-height);
    border: var(--hairline) solid transparent;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }

  .icon:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .icon.on {
    border-color: var(--accent);
    background: var(--selection-bg);
    color: var(--accent);
  }

  .icon:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
</style>
