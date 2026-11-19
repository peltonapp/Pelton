<script lang="ts">
  // the left pane of the contacts space: the address books, and the
  // organizations found among the contacts of the book on screen.
  import { createEventDispatcher } from 'svelte'
  import Ph from '../common/Ph.svelte'
  import bookIcon from '@phosphor-icons/core/assets/duotone/address-book-duotone.svg?raw'
  import buildingIcon from '@phosphor-icons/core/assets/duotone/buildings-duotone.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import { addressBooks, contacts, contactBookFilter, showBook } from '../../stores/contacts'
  import { contactOrg } from '../../stores/contactsview'
  import { organizationCounts } from '../../lib/contactlist'
  import { t } from '../../lib/i18n'

  const dispatch = createEventDispatcher<{ manage: void; pick: void }>()

  $: organizations = organizationCounts($contacts)

  function pickBook(id: number): void {
    contactOrg.set(null)
    void showBook(id)
    dispatch('pick')
  }

  function pickOrg(name: string): void {
    contactOrg.update((current) => (current?.toLocaleLowerCase() === name.toLocaleLowerCase() ? null : name))
    dispatch('pick')
  }

  function needsAttention(book: { lastError: string; hasPassword: boolean }): boolean {
    return book.lastError !== '' || !book.hasPassword
  }
</script>

<nav class="side" aria-label={$t('contacts.side.label')}>
  <h3 class="head">{$t('contacts.side.books')}</h3>
  <ul>
    <li>
      <button
        type="button"
        class="item"
        class:on={$contactBookFilter === 0 && $contactOrg === null}
        aria-current={$contactBookFilter === 0 && $contactOrg === null ? 'true' : undefined}
        on:click={() => pickBook(0)}
      >
        <Ph svg={bookIcon} size={16} />
        <span class="name">{$t('contacts.allBooks')}</span>
        <span class="count">{$addressBooks.reduce((sum, b) => sum + b.contactCount, 0)}</span>
      </button>
    </li>
    {#each $addressBooks as book (book.id)}
      <li>
        <button
          type="button"
          class="item"
          class:on={$contactBookFilter === book.id && $contactOrg === null}
          aria-current={$contactBookFilter === book.id && $contactOrg === null ? 'true' : undefined}
          on:click={() => pickBook(book.id)}
        >
          <Ph svg={bookIcon} size={16} />
          <span class="name">{book.name || $t('contacts.book.untitled')}</span>
          {#if needsAttention(book)}
            <span class="warn" title={book.hasPassword ? book.lastError : $t('contacts.bookNeedsPassword').replace('{name}', book.name)}>
              <Ph svg={warningIcon} size={14} />
            </span>
          {/if}
          <span class="count">{book.contactCount}</span>
        </button>
      </li>
    {/each}
  </ul>

  {#if organizations.length > 0}
    <h3 class="head">{$t('contacts.side.organizations')}</h3>
    <ul>
      {#each organizations as org (org.name)}
        <li>
          <button
            type="button"
            class="item"
            class:on={$contactOrg?.toLocaleLowerCase() === org.name.toLocaleLowerCase()}
            aria-current={$contactOrg?.toLocaleLowerCase() === org.name.toLocaleLowerCase() ? 'true' : undefined}
            on:click={() => pickOrg(org.name)}
          >
            <Ph svg={buildingIcon} size={16} />
            <span class="name">{org.name}</span>
            <span class="count">{org.count}</span>
          </button>
        </li>
      {/each}
    </ul>
  {/if}

  <button type="button" class="manage" on:click={() => dispatch('manage')}>{$t('contacts.manageBooks')}</button>
</nav>

<style>
  .side {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-height: 0;
    padding: var(--space-3) var(--space-2);
    overflow-y: auto;
    background: var(--surface-sunken);
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .head {
    margin: var(--space-3) var(--space-2) var(--space-1);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    color: var(--text-tertiary);
  }

  .head:first-child {
    margin-top: 0;
  }

  .item {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    min-height: var(--control-height);
    padding: 0 var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    text-align: start;
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .item:hover {
    background: var(--surface-hover);
  }

  .item.on {
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .count {
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
    font-variant-numeric: tabular-nums;
  }

  .warn {
    display: inline-flex;
    color: var(--warning);
  }

  .manage {
    margin-top: var(--space-3);
    padding: var(--space-1) var(--space-2);
    border: none;
    background: transparent;
    color: var(--accent);
    font: inherit;
    font-size: var(--fz-meta);
    text-align: start;
    cursor: var(--cursor-action);
  }

  .manage:hover {
    text-decoration: underline;
  }

  @media (prefers-reduced-motion: reduce) {
    .item {
      transition: none;
    }
  }
</style>
