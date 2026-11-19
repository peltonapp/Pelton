<script lang="ts">
  // the contacts screen (#168): the address books the user keeps on their own
  // server, listed here and editable. It opens over the mail view like the
  // settings panel does, and is code-split the same way, so an install with no
  // address book never pays for it.
  import { createEventDispatcher, onMount } from 'svelte'
  import {
    IconX,
    IconPlus,
    IconRefresh,
    IconUser,
    IconTrash,
    IconPencil,
    IconMail,
    IconPhone,
    IconAddressBook,
  } from '@tabler/icons-svelte'
  import Spinner from '../common/Spinner.svelte'
  import ContactEditor from './ContactEditor.svelte'
  import ContactConflictDialog from './ContactConflictDialog.svelte'
  import {
    addressBooks,
    contacts,
    contactBookFilter,
    contactsLoading,
    contactConflict,
    loadContacts,
    showBook,
    refreshContacts,
    removeContact,
    contactSeed,
  } from '../../stores/contacts'
  import { openSettingsAt } from '../../stores/settingsnav'
  import { formatRelative } from '../../lib/format'
  import { t } from '../../lib/i18n'
  import type { Contact, ContactDraft } from '../../lib/types'

  // writing to a contact is App.svelte's job: it owns the compose sessions and
  // knows which account a new message starts from.
  const dispatch = createEventDispatcher<{ close: void; compose: string }>()

  // the contact shown in the detail pane, or null on an empty selection.
  let selected: Contact | null = null
  // the contact being edited, or null. A draft with id 0 is a new one.
  let editing: ContactDraft | null = null
  // the contact whose delete is waiting on a confirm.
  let confirmingId: number | null = null
  let search = ''

  onMount(loadContacts)

  // the filter is a plain substring over the name, the addresses and the
  // organisation, which is what people actually type when hunting a contact.
  $: filtered = search.trim()
    ? $contacts.filter((c) => matches(c, search.trim().toLowerCase()))
    : $contacts

  $: if (selected && !$contacts.some((c) => c.id === selected?.id)) {
    // the selected contact was deleted or synced away.
    selected = null
  }

  // a book that has never synced or that failed is worth saying so about,
  // since an empty list otherwise looks like an empty address book.
  $: troubled = $addressBooks.filter((b) => b.lastError !== '' || !b.hasPassword)

  function matches(contact: Contact, needle: string): boolean {
    if (contact.fullName.toLowerCase().includes(needle)) {
      return true
    }
    if (contact.organization.toLowerCase().includes(needle)) {
      return true
    }
    return contact.emails.some((e) => e.value.toLowerCase().includes(needle))
  }

  function startCreate(): void {
    const book = $addressBooks.find((b) => !b.readOnly)
    editing = {
      id: 0,
      // no default book: a contact filed in the wrong address book is a
      // contact on the wrong device, so the editor asks. This only preselects
      // when there is exactly one book it could mean.
      bookId: $addressBooks.filter((b) => !b.readOnly).length === 1 ? (book?.id ?? 0) : 0,
      fullName: '',
      organization: '',
      title: '',
      note: '',
      emails: [{ value: '', label: '' }],
      phones: [],
      force: false,
    }
  }

  // a person handed over from elsewhere opens the editor already filled in.
  $: if ($contactSeed) {
    startCreate()
    if (editing) {
      editing = { ...editing, fullName: $contactSeed.name, emails: [{ value: $contactSeed.email, label: '' }] }
    }
    contactSeed.set(null)
  }

  function startEdit(contact: Contact): void {
    editing = {
      id: contact.id,
      bookId: contact.bookId,
      fullName: contact.fullName,
      organization: contact.organization,
      title: contact.title,
      note: contact.note,
      emails: contact.emails.length ? contact.emails.map((e) => ({ ...e })) : [{ value: '', label: '' }],
      phones: contact.phones.map((p) => ({ ...p })),
      force: false,
    }
  }

  async function confirmDelete(id: number): Promise<void> {
    confirmingId = null
    await removeContact(id)
  }

  function writeTo(email: string): void {
    dispatch('compose', email)
  }

  function bookName(id: number): string {
    return $addressBooks.find((b) => b.id === id)?.name || $t('contacts.book.untitled')
  }
</script>

<section class="screen" aria-label={$t('contacts.title')}>
  <header class="head">
    <div class="title">
      <IconAddressBook size={18} stroke={1.7} />
      <h2>{$t('contacts.title')}</h2>
    </div>
    <div class="actions">
      <button type="button" class="ghost" on:click={refreshContacts} disabled={$contactsLoading}>
        <IconRefresh size={14} stroke={1.8} />
        {$t('contacts.sync')}
      </button>
      <button type="button" class="primary" on:click={startCreate} disabled={$addressBooks.length === 0}>
        <IconPlus size={14} stroke={2} />
        {$t('contacts.new')}
      </button>
      <button type="button" class="icon" aria-label={$t('action.close')} on:click={() => dispatch('close')}>
        <IconX size={16} stroke={1.8} />
      </button>
    </div>
  </header>

  {#if $addressBooks.length === 0}
    <div class="empty">
      <p>{$t('contacts.noBooks')}</p>
      <button type="button" class="primary" on:click={() => { openSettingsAt('contacts'); dispatch('close') }}>
        {$t('contacts.addBook')}
      </button>
    </div>
  {:else}
    <div class="body">
      <aside class="books">
        <button type="button" class="book" class:on={$contactBookFilter === 0} on:click={() => showBook(0)}>
          <span class="book-name">{$t('contacts.allBooks')}</span>
          <span class="count">{$contacts.length}</span>
        </button>
        {#each $addressBooks as book (book.id)}
          <button type="button" class="book" class:on={$contactBookFilter === book.id} on:click={() => showBook(book.id)}>
            <span class="book-name">{book.name || $t('contacts.book.untitled')}</span>
            <span class="count">{book.contactCount}</span>
          </button>
        {/each}
        <button type="button" class="manage" on:click={() => { openSettingsAt('contacts'); dispatch('close') }}>
          {$t('contacts.manageBooks')}
        </button>
      </aside>

      <div class="list-pane">
        <input
          type="search"
          class="search"
          bind:value={search}
          placeholder={$t('contacts.searchPlaceholder')}
          aria-label={$t('contacts.searchPlaceholder')}
        />
        {#each troubled as book (book.id)}
          <p class="book-problem">
            {book.hasPassword
              ? $t('contacts.bookFailed').replace('{name}', book.name || $t('contacts.book.untitled'))
              : $t('contacts.bookNeedsPassword').replace('{name}', book.name || $t('contacts.book.untitled'))}
          </p>
        {/each}

        {#if $contactsLoading && $contacts.length === 0}
          <div class="loading"><Spinner /></div>
        {:else if filtered.length === 0}
          <p class="empty-list">{search ? $t('contacts.noMatches') : $t('contacts.emptyBook')}</p>
        {:else}
          <ul class="contacts">
            {#each filtered as contact (contact.id)}
              <li>
                <button
                  type="button"
                  class="row"
                  class:on={selected?.id === contact.id}
                  on:click={() => (selected = contact)}
                >
                  <IconUser size={15} stroke={1.6} />
                  <span class="row-name">{contact.fullName}</span>
                  <span class="row-mail">{contact.emails[0]?.value ?? ''}</span>
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </div>

      <div class="detail">
        {#if selected}
          <div class="detail-head">
            <div>
              <h3>{selected.fullName}</h3>
              {#if selected.title || selected.organization}
                <p class="sub">{[selected.title, selected.organization].filter(Boolean).join(' · ')}</p>
              {/if}
              <p class="sub">{bookName(selected.bookId)}</p>
            </div>
            {#if !selected.readOnly}
              <div class="detail-actions">
                <button type="button" class="icon" aria-label={$t('action.edit')} on:click={() => selected && startEdit(selected)}>
                  <IconPencil size={15} stroke={1.7} />
                </button>
                <button type="button" class="icon del" aria-label={$t('action.delete')} on:click={() => (confirmingId = selected?.id ?? null)}>
                  <IconTrash size={15} stroke={1.7} />
                </button>
              </div>
            {/if}
          </div>

          {#if confirmingId === selected.id}
            <div class="confirm">
              <span>{$t('contacts.deleteConfirm')}</span>
              <button type="button" class="danger" on:click={() => selected && confirmDelete(selected.id)}>
                {$t('action.delete')}
              </button>
              <button type="button" class="ghost" on:click={() => (confirmingId = null)}>{$t('mailboxes.cancel')}</button>
            </div>
          {/if}

          {#each selected.emails as email (email.value)}
            <div class="value-row">
              <IconMail size={14} stroke={1.6} />
              <button type="button" class="link" on:click={() => writeTo(email.value)}>{email.value}</button>
              {#if email.label}<span class="label">{email.label}</span>{/if}
            </div>
          {/each}
          {#each selected.phones as phone (phone.value)}
            <div class="value-row">
              <IconPhone size={14} stroke={1.6} />
              <span>{phone.value}</span>
              {#if phone.label}<span class="label">{phone.label}</span>{/if}
            </div>
          {/each}

          {#if selected.note}
            <p class="note">{selected.note}</p>
          {/if}
          {#if selected.extra.length > 0}
            <p class="extra">{$t('contacts.extraFields').replace('{fields}', selected.extra.join(', '))}</p>
          {/if}
          {#if selected.updated}
            <p class="sub">{$t('contacts.updated').replace('{when}', formatRelative(Date.parse(selected.updated), $t))}</p>
          {/if}
          {#if selected.readOnly}
            <p class="sub">{$t('contacts.readOnly')}</p>
          {/if}
        {:else}
          <p class="empty-list">{$t('contacts.pickOne')}</p>
        {/if}
      </div>
    </div>
  {/if}
</section>

{#if editing}
  <ContactEditor draft={editing} on:close={() => (editing = null)} />
{/if}

{#if $contactConflict}
  <ContactConflictDialog conflict={$contactConflict} />
{/if}

<style>
  .screen {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: flex;
    flex-direction: column;
    background: var(--surface-base);
    color: var(--text-primary);
    font-family: var(--font-ui);
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .title {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  h2 {
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
  }

  .actions {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .body {
    flex: 1;
    display: grid;
    grid-template-columns: 200px minmax(220px, 1fr) minmax(260px, 1.4fr);
    min-height: 0;
  }

  .books {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--space-3);
    border-inline-end: var(--hairline) solid var(--border-subtle);
    overflow-y: auto;
  }

  .book {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    text-align: start;
  }

  .book:hover {
    background: var(--surface-hover);
  }

  .book.on {
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .book-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .count {
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .manage {
    margin-top: var(--space-2);
    border: none;
    background: transparent;
    color: var(--accent);
    font: inherit;
    font-size: var(--fz-meta);
    text-align: start;
    padding: var(--space-1) var(--space-2);
    cursor: var(--cursor-action);
  }

  .list-pane {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-3);
    border-inline-end: var(--hairline) solid var(--border-subtle);
    overflow-y: auto;
  }

  .search {
    height: var(--control-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
  }

  .contacts {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .row {
    display: grid;
    grid-template-columns: auto 1fr;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
  }

  .row:hover {
    background: var(--surface-hover);
  }

  .row.on {
    background: var(--selection-bg);
  }

  .row-name {
    font-size: var(--fz-list);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row-mail {
    grid-column: 2;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .detail {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-4);
    overflow-y: auto;
  }

  .detail-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-3);
  }

  .detail-actions {
    display: flex;
    gap: var(--space-1);
  }

  h3 {
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
  }

  .sub {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .value-row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-body);
  }

  .label {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .link {
    border: none;
    background: transparent;
    color: var(--link);
    font: inherit;
    padding: 0;
    cursor: var(--cursor-action);
  }

  .link:hover {
    text-decoration: underline;
  }

  .note {
    margin: 0;
    white-space: pre-wrap;
    font-size: var(--fz-body);
    color: var(--text-secondary);
  }

  .extra {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .book-problem {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--warning);
  }

  .empty,
  .loading,
  .empty-list {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    flex: 1;
    color: var(--text-tertiary);
    font-size: var(--fz-body);
    padding: var(--space-5);
  }

  .confirm {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--danger);
  }

  button.primary,
  button.ghost,
  button.danger {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    height: var(--control-height);
    padding: 0 var(--space-3);
    /* a label is one line whatever the locale: German runs half again as long
       as English and was wrapping inside the fixed height. */
    white-space: nowrap;
    border-radius: var(--radius-control);
    border: var(--hairline) solid var(--border-default);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  button.primary {
    background: var(--accent);
    color: var(--accent-fg);
    border-color: transparent;
  }

  button.danger {
    background: var(--danger);
    color: var(--text-inverse);
    border-color: transparent;
  }

  button:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--control-height);
    height: var(--control-height);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }

  .icon:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .icon.del:hover {
    color: var(--danger);
  }
</style>
