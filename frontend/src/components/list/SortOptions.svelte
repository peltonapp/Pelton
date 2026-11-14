<script lang="ts">
  // the search sort choices as menu items: automatic (with what it resolves to
  // right now) and every order the backend offers.
  import { createEventDispatcher } from 'svelte'
  import checkIcon from '@phosphor-icons/core/assets/duotone/check-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { t } from '../../lib/i18n'
  import { searchSorts, automaticSort } from '../../lib/searchsort'
  import type { SearchSortPref } from '../../lib/types'
  import { activeSortPref, searchSortKind } from '../../stores/messages'

  const dispatch = createEventDispatcher<{ pick: SearchSortPref }>()

  $: autoLabel = $searchSortKind ? $t(`messageList.search.sort.${automaticSort($searchSortKind)}`) : ''
  $: options = ['auto', ...searchSorts] as SearchSortPref[]
</script>

<span class="label">{$t('messageList.search.sortBy')}</span>
{#each options as option (option)}
  <button type="button" class="opt" role="menuitemradio" aria-checked={$activeSortPref === option} on:click={() => dispatch('pick', option)}>
    <span class="check">{#if $activeSortPref === option}<Ph svg={checkIcon} size={13} />{/if}</span>
    <span class="name">{$t(`messageList.search.sort.${option}`)}</span>
    {#if option === 'auto'}<span class="note">{autoLabel}</span>{/if}
  </button>
{/each}

<style>
  .label {
    display: block;
    padding: var(--space-2) var(--space-3) var(--space-1);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    color: var(--text-tertiary);
  }

  .opt {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    padding: var(--space-2) var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font-size: var(--fz-label);
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .opt:hover,
  .opt:focus-visible {
    background: var(--surface-hover);
    outline: none;
  }

  .check {
    display: inline-flex;
    justify-content: center;
    width: var(--space-5);
    flex-shrink: 0;
    color: var(--accent);
  }

  .name {
    flex: 1;
  }

  .note {
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }
</style>
