<script lang="ts">
  // the composer's snippet button and its searchable list. typing narrows the
  // list, the arrows move through it and Enter inserts the one lit; each row
  // shows its trigger so the faster way in is learnt along the way.
  import { createEventDispatcher, tick } from 'svelte'
  import snippetIcon from '@phosphor-icons/core/assets/duotone/text-indent-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import AnchoredPopover from '../../common/AnchoredPopover.svelte'
  import { snippets } from '../../../stores/snippets'
  import { filterSnippets, triggerPrefix } from '../../../lib/snippets'
  import { shortcutTitle } from '../../../stores/shortcuts'
  import { openSettingsAt } from '../../../stores/settingsnav'
  import { tip } from '../../../lib/tip'
  import { t } from '../../../lib/i18n'
  import type { Snippet } from '../../../lib/types'

  /** Unique id base for the list. */
  export let id: string
  /** Shows only the icon, for the docked footer. */
  export let compact = false
  /** Opens the list above the button rather than below. */
  export let menuAbove = false
  /** Offers a way to the settings page, which only the main window can open. */
  export let canManage = false

  const dispatch = createEventDispatcher<{ pick: Snippet }>()

  let button: HTMLButtonElement
  let input: HTMLInputElement
  let open = false
  let query = ''
  let lit = 0

  $: shown = filterSnippets($snippets, query)
  $: if (lit >= shown.length) {
    lit = Math.max(0, shown.length - 1)
  }

  /** Opens the list with the search field focused, as the shortcut does. */
  export async function show(): Promise<void> {
    open = true
    query = ''
    lit = 0
    await tick()
    input?.focus()
  }

  function pick(snippet: Snippet): void {
    open = false
    dispatch('pick', snippet)
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      if (shown.length > 0) {
        lit = (lit + (event.key === 'ArrowDown' ? 1 : -1) + shown.length) % shown.length
      }
    } else if (event.key === 'Enter') {
      event.preventDefault()
      const chosen = shown[lit]
      if (chosen) {
        pick(chosen)
      }
    }
  }

  function manage(): void {
    open = false
    openSettingsAt('snippets')
  }
</script>

<button
  type="button"
  class="c2-tool"
  class:compact
  bind:this={button}
  aria-label={$t('snippets.insert')}
  aria-haspopup="dialog"
  aria-expanded={open}
  use:tip={{ text: $shortcutTitle($t('snippets.insert'), 'compose-insert-snippet'), side: menuAbove ? 'top' : 'bottom' }}
  on:click={() => (open ? (open = false) : void show())}
>
  <Ph svg={snippetIcon} size={16} />
  {#if !compact}
    <span>{$t('snippets.button')}</span>
  {/if}
</button>

{#if open}
  <AnchoredPopover anchor={button} side={menuAbove ? 'above' : 'below'} align="start" label={$t('snippets.insert')} on:close={() => (open = false)}>
    <div class="picker">
      <input
        bind:this={input}
        bind:value={query}
        type="search"
        autocomplete="off"
        placeholder={$t('snippets.search')}
        aria-label={$t('snippets.search')}
        aria-controls={`${id}-snippets`}
        aria-activedescendant={shown[lit] ? `${id}-snippet-${shown[lit].id}` : undefined}
        on:keydown={onKeydown}
      />
      <ul id={`${id}-snippets`} class="list" role="listbox" aria-label={$t('snippets.insert')}>
        {#each shown as snippet, i (snippet.id)}
          <li
            id={`${id}-snippet-${snippet.id}`}
            role="option"
            aria-selected={i === lit}
            class:lit={i === lit}
            on:mousedown|preventDefault={() => pick(snippet)}
            on:mouseenter={() => (lit = i)}
          >
            <span class="name">{snippet.name}</span>
            {#if snippet.trigger}
              <kbd>{triggerPrefix}{snippet.trigger}</kbd>
            {/if}
            <span class="body">{snippet.body}</span>
          </li>
        {:else}
          <li class="empty" role="presentation">
            {$snippets.length === 0 ? $t('snippets.none') : $t('snippets.noMatch')}
          </li>
        {/each}
      </ul>
      {#if canManage}
        <button type="button" class="manage" on:click={manage}>{$t('snippets.manage')}</button>
      {/if}
    </div>
  </AnchoredPopover>
{/if}

<style>
  .picker {
    display: flex;
    flex-direction: column;
    width: calc(var(--popover-width) - var(--space-4));
    padding: var(--space-2);
    gap: var(--space-2);
  }

  input {
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-primary);
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    outline: none;
  }

  input:focus {
    border-color: var(--accent);
  }

  .list {
    display: flex;
    flex-direction: column;
    max-height: var(--snippet-list-height);
    margin: 0;
    padding: 0;
    overflow-y: auto;
    list-style: none;
  }

  li[role='option'] {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: var(--space-1) var(--space-3);
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  li.lit {
    background: var(--selection-bg);
  }

  .name {
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  kbd {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .body {
    grid-column: 1 / -1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .empty {
    padding: var(--space-4) var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    text-align: center;
  }

  .manage {
    align-self: flex-start;
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font-family: var(--font-ui);
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
  }

  .manage:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
</style>
