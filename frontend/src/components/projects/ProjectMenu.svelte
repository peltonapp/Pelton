<script lang="ts">
  // a button that picks a project, with a menu of the active ones. the
  // composer uses it to file what is being written, the event popover to file
  // an event. unset, it is a quiet icon; set, it names the project.
  import { createEventDispatcher } from 'svelte'
  import kanbanIcon from '@phosphor-icons/core/assets/duotone/kanban-duotone.svg?raw'
  import checkIcon from '@phosphor-icons/core/assets/regular/check.svg?raw'
  import '../../theme/controls.css'
  import Ph from '../common/Ph.svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import { projectColor } from '../../lib/projectlogic'
  import { tip } from '../../lib/tip'
  import { activeProjects, loadProjects, projects } from '../../stores/projects'
  import { t } from '../../lib/i18n'

  /** The chosen project's id, 0 for none. */
  export let value = 0
  /** Shows only the icon while nothing is chosen. */
  export let compact = false
  /** Opens the menu above the button. */
  export let menuAbove = false
  /** The class the button wears, to match the controls around it. */
  export let buttonClass = 'c2-tool'

  const dispatch = createEventDispatcher<{ change: number }>()

  let button: HTMLButtonElement
  let open = false
  let query = ''

  $: chosen = $projects.find((p) => p.id === value) ?? null
  $: label = chosen ? chosen.name : $t('projects.compose.button')
  $: list = $activeProjects.filter((p) => !query.trim() || p.name.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()))

  async function toggle(): Promise<void> {
    if (open) {
      open = false
      return
    }
    query = ''
    open = true
    if ($projects.length === 0) {
      await loadProjects()
    }
  }

  function pick(id: number): void {
    open = false
    dispatch('change', id)
  }
</script>

<button
  type="button"
  class={buttonClass}
  class:compact={compact && !chosen}
  class:set={!!chosen}
  bind:this={button}
  aria-label={chosen ? $t('projects.compose.filedUnder').replace('{name}', chosen.name) : $t('projects.compose.button')}
  aria-haspopup="menu"
  aria-expanded={open}
  use:tip={{ text: $t('projects.compose.tip'), side: menuAbove ? 'top' : 'bottom' }}
  on:click={() => void toggle()}
>
  {#if chosen}
    <span class="dot" style={`background:${projectColor(chosen)}`} aria-hidden="true"></span>
  {:else}
    <Ph svg={kanbanIcon} size={16} />
  {/if}
  {#if !compact || chosen}
    <span class="label">{label}</span>
  {/if}
</button>

{#if open}
  <AnchoredPopover anchor={button} side={menuAbove ? 'above' : 'below'} align="start" role="menu" label={$t('projects.compose.button')} on:close={() => (open = false)}>
    <div class="menu">
      <p class="lead">{$t('projects.compose.lead')}</p>
      {#if $activeProjects.length > 8}
        <!-- svelte-ignore a11y-autofocus -->
        <input class="cal-field find" bind:value={query} placeholder={$t('projects.pickPlaceholder')} aria-label={$t('projects.pickPlaceholder')} autofocus />
      {/if}
      <div class="items">
        {#each list as p (p.id)}
          <button type="button" class="item" role="menuitemradio" aria-checked={p.id === value} on:click={() => pick(p.id)}>
            <span class="dot" style={`background:${projectColor(p)}`} aria-hidden="true"></span>
            <span class="name">{p.name}</span>
            {#if p.id === value}<Ph svg={checkIcon} size={13} />{/if}
          </button>
        {:else}
          <p class="none">{$activeProjects.length === 0 ? $t('projects.compose.none') : $t('projects.noMatches')}</p>
        {/each}
      </div>
      {#if value}
        <button type="button" class="item clear" role="menuitem" on:click={() => pick(0)}>{$t('projects.compose.clear')}</button>
      {/if}
    </div>
  </AnchoredPopover>
{/if}

<style>
  .set {
    color: var(--text-primary);
    background: var(--surface-hover);
  }

  .label {
    max-width: calc(var(--space-6) * 6);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .dot {
    flex-shrink: 0;
    width: var(--space-2);
    height: var(--space-2);
    border-radius: 50%;
  }

  .menu {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    width: calc(var(--popover-width) - var(--space-6) * 2);
    padding: var(--space-2);
  }

  .lead {
    margin: 0;
    padding: var(--space-2) var(--space-3) var(--space-1);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .find {
    margin: 0 var(--space-1) var(--space-1);
  }

  .items {
    display: flex;
    flex-direction: column;
    max-height: var(--snippet-list-height);
    overflow-y: auto;
  }

  .item {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .item:hover,
  .item:focus-visible {
    background: var(--surface-hover);
    outline: none;
  }

  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .clear {
    color: var(--text-secondary);
    border-top: var(--hairline) solid var(--border-subtle);
    border-radius: 0;
    margin-top: var(--space-1);
    padding-top: var(--space-3);
  }

  .none {
    margin: var(--space-2) var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }
</style>
