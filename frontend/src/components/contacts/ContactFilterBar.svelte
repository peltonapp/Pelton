<script lang="ts">
  // the search box and the sort choice that sit above the contact list and the
  // card grid. Both write to the contacts view store.
  import Ph from '../common/Ph.svelte'
  import Select from '../common/Select.svelte'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
  import { contactQuery, contactSort } from '../../stores/contactsview'
  import { t } from '../../lib/i18n'
  import type { ContactSort } from '../../lib/contactlist'

  $: sorts = [
    { value: 'name', label: $t('contacts.sort.name') },
    { value: 'nameDesc', label: $t('contacts.sort.nameDesc') },
    { value: 'organization', label: $t('contacts.sort.organization') },
    { value: 'recent', label: $t('contacts.sort.recent') },
  ]
</script>

<div class="bar">
  <label class="search">
    <Ph svg={searchIcon} size={16} />
    <input
      type="search"
      bind:value={$contactQuery}
      placeholder={$t('contacts.searchPlaceholder')}
      aria-label={$t('contacts.searchPlaceholder')}
    />
  </label>
  <div class="sort">
    <Select
      value={$contactSort}
      items={sorts}
      ariaLabel={$t('contacts.sort.label')}
      on:change={(e) => contactSort.set(e.detail as ContactSort)}
    />
  </div>
</div>

<style>
  .bar {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .search {
    display: flex;
    flex: 1;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
    height: var(--control-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-tertiary);
    transition: border-color var(--motion-fast) var(--ease-out);
  }

  .search:focus-within {
    border-color: var(--accent);
  }

  input {
    flex: 1;
    min-width: 0;
    height: 100%;
    border: none;
    outline: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
  }

  input::-webkit-search-cancel-button {
    appearance: none;
  }

  .sort {
    flex-shrink: 0;
    min-width: 0;
  }

  .sort :global(.select) {
    height: var(--control-height);
    font-size: var(--fz-label);
  }

  @media (prefers-reduced-motion: reduce) {
    .search {
      transition: none;
    }
  }
</style>
