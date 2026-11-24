<script lang="ts">
  // a grid of the icons a script can wear on a button, from the app's set.
  import { createEventDispatcher } from 'svelte'
  import Ph from '../common/Ph.svelte'
  import { t } from '../../lib/i18n'
  import { defaultScriptIcon, scriptIcons } from '../../lib/scripticons'

  /** The picked icon's name. */
  export let value: unknown = defaultScriptIcon
  /** The group's accessible name. */
  export let label: string

  const dispatch = createEventDispatcher<{ change: string }>()
  const names = Object.keys(scriptIcons)

  let group: HTMLDivElement

  // the picked icon is the one tab stops on; with none picked, the first.
  $: stop = typeof value === 'string' && names.includes(value) ? value : names[0]

  function onKey(e: KeyboardEvent): void {
    const at = names.indexOf(typeof value === 'string' ? value : stop)
    let to = -1
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
      to = (at + 1) % names.length
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
      to = (at - 1 + names.length) % names.length
    } else if (e.key === 'Home') {
      to = 0
    } else if (e.key === 'End') {
      to = names.length - 1
    }
    if (to < 0) {
      return
    }
    e.preventDefault()
    dispatch('change', names[to])
    group.querySelectorAll<HTMLElement>('[role="radio"]')[to]?.focus()
  }
</script>

<!-- svelte-ignore a11y-interactive-supports-focus -->
<div class="icons" role="radiogroup" aria-label={label} bind:this={group} on:keydown={onKey}>
  {#each Object.entries(scriptIcons) as [name, svg] (name)}
    {@const text = $t(`scripts.icon.${name}`)}
    <button type="button" class="icon" class:on={value === name} role="radio" aria-checked={value === name} tabindex={stop === name ? 0 : -1} aria-label={text} title={text} on:click={() => dispatch('change', name)}>
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
