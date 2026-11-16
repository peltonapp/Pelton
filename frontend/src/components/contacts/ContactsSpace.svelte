<script lang="ts">
  // the contacts space of the new design: an address book in three panes (books
  // and organizations, the contact list, the contact), or the same contacts as a
  // card grid. The classic design keeps ContactsScreen. The panes fold as the
  // window narrows: the book pane becomes a drawer first, then the list and the
  // contact take turns.
  import { createEventDispatcher, onMount } from 'svelte'
  import { fade, fly } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import Ph from '../common/Ph.svelte'
  import ContactSidebar from './ContactSidebar.svelte'
  import ContactList from './ContactList.svelte'
  import ContactGrid from './ContactGrid.svelte'
  import ContactPane from './ContactPane.svelte'
  import ContactConflictDialog from './ContactConflictDialog.svelte'
  import bookIcon from '@phosphor-icons/core/assets/duotone/address-book-duotone.svg?raw'
  import syncIcon from '@phosphor-icons/core/assets/duotone/arrows-clockwise-duotone.svg?raw'
  import plusIcon from '@phosphor-icons/core/assets/duotone/plus-duotone.svg?raw'
  import listIcon from '@phosphor-icons/core/assets/duotone/list-bullets-duotone.svg?raw'
  import gridIcon from '@phosphor-icons/core/assets/duotone/squares-four-duotone.svg?raw'
  import sidebarIcon from '@phosphor-icons/core/assets/duotone/sidebar-simple-duotone.svg?raw'
  import {
    addressBooks,
    contacts,
    contactBookFilter,
    contactsLoading,
    contactConflict,
    loadContacts,
    refreshContacts,
    contactSeed,
  } from '../../stores/contacts'
  import { contactOrg, selectedContactId } from '../../stores/contactsview'
  import { sidebar } from '../../stores/accounts'
  import { askConfirm } from '../../stores/confirm'
  import { openSettingsAt } from '../../stores/settingsnav'
  import { openSpace, registerSpaceActions } from '../../stores/space'
  import { focusSpaceSearch, matchesQuery, registerSpaceSearch } from '../../lib/spacesearch'

  let root: HTMLElement | undefined
  import { revealMessage } from '../../stores/selection'
  import { prefs, setContactsView } from '../../stores/prefs'
  import { organizationCounts } from '../../lib/contactlist'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { Contact, ContactDraft, ContactsView, MessageSummary } from '../../lib/types'

  // writing to a contact is App.svelte's job: it owns the compose sessions and
  // knows which account a new message starts from.
  const dispatch = createEventDispatcher<{ compose: string }>()

  // widths, in px, below which the panes fold.
  const wideFrom = 900
  const mediumFrom = 600

  let width = 0
  let ready = false
  // the contact being edited, or null. A draft with id 0 is a new one.
  let editing: ContactDraft | null = null
  let dirty = false
  let sideOpen = false

  $: view = $prefs.contactsView as ContactsView
  // an unmeasured window (width 0) is treated as wide.
  $: mode = width === 0 || width >= wideFrom ? 'wide' : width >= mediumFrom ? 'medium' : 'narrow'
  $: if (mode === 'wide') {
    sideOpen = false
  }

  $: selected = $contacts.find((c) => c.id === $selectedContactId) ?? null
  $: if ($selectedContactId !== null && selected === null && !$contactsLoading) {
    selectedContactId.set(null)
  }
  $: paneOpen = editing !== null || selected !== null

  // an organization that no longer exists in the book on screen would leave
  // the list empty with nothing to say why.
  $: if ($contactOrg !== null && !organizationCounts($contacts).some((o) => o.name.toLocaleLowerCase() === $contactOrg?.toLocaleLowerCase())) {
    contactOrg.set(null)
  }

  // a book that has never synced or that failed is worth saying so about,
  // since an empty list otherwise looks like an empty address book.
  $: troubled = $addressBooks.filter((b) => b.lastError !== '' || !b.hasPassword)

  $: layout = view === 'grid' ? `grid-${mode}` : `panes-${mode}`

  onMount(() => {
    void loadContacts().then(() => (ready = true))
    // while contacts is on screen the app's "new" key makes a contact and its
    // search key goes to the contact search.
    const stopSearch = registerSpaceSearch('contacts', {
      placeholderKey: 'contacts.searchPlaceholder',
      emptyKey: 'contacts.noMatches',
      search: (query) =>
        $contacts
          .filter((c) =>
            matchesQuery(query, [
              c.fullName,
              c.organization,
              c.title,
              ...c.emails.map((e) => e.value),
              ...c.phones.map((p) => p.value),
            ]),
          )
          .slice(0, 20)
          .map((c) => ({
            id: String(c.id),
            title: c.fullName || c.emails[0]?.value || c.organization,
            detail: c.emails[0]?.value ?? c.phones[0]?.value ?? '',
            aside: c.organization,
          })),
      open: (result) => void select(Number(result.id)),
    })
    const stopActions = registerSpaceActions('contacts', {
      compose: () => void startCreate(),
      search: () => {
        if (focusSpaceSearch()) {
          return
        }
        const input = root?.querySelector<HTMLInputElement>('input[type="search"]')
        if (!input) {
          return false
        }
        input.focus()
        input.select()
      },
    })
    return () => {
      stopSearch()
      stopActions()
    }
  })

  // a person handed over from elsewhere opens the editor already filled in.
  $: if ($contactSeed && ready) {
    const seed = $contactSeed
    contactSeed.set(null)
    void startCreate(seed)
  }

  // whichever way a conflict is settled, what was typed is no longer the thing
  // to edit: the server's version replaced it, or it was written over the server.
  let hadConflict = false
  $: {
    if (hadConflict && $contactConflict === null) {
      editing = null
      dirty = false
    }
    hadConflict = $contactConflict !== null
  }

  async function leaveEditor(): Promise<boolean> {
    if (editing && dirty) {
      const discard = await askConfirm({
        title: $t('modal.discardTitle'),
        body: $t('modal.discardHint'),
        confirmLabel: $t('action.discard'),
        danger: true,
      })
      if (!discard) {
        return false
      }
    }
    editing = null
    dirty = false
    return true
  }

  async function select(id: number): Promise<void> {
    if (await leaveEditor()) {
      selectedContactId.set(id)
    }
  }

  async function dismissPane(): Promise<void> {
    if (await leaveEditor()) {
      selectedContactId.set(null)
    }
  }

  function closeEditor(): void {
    editing = null
    dirty = false
  }

  async function startCreate(seed?: { name: string; email: string }): Promise<void> {
    if (!(await leaveEditor())) {
      return
    }
    const writable = $addressBooks.filter((b) => !b.readOnly)
    // no default book: a contact filed in the wrong address book is a contact on
    // the wrong device, so the editor asks. This only preselects when there is
    // exactly one book it could mean, or the one on screen.
    const shown = writable.find((b) => b.id === $contactBookFilter)
    const book = shown ?? (writable.length === 1 ? writable[0] : undefined)
    editing = {
      id: 0,
      bookId: book?.id ?? 0,
      fullName: seed?.name ?? '',
      organization: '',
      title: '',
      note: '',
      emails: [{ value: seed?.email ?? '', label: '' }],
      phones: [],
      matrixId: '',
      force: false,
    }
  }

  async function startEdit(contact: Contact): Promise<void> {
    if (!(await leaveEditor())) {
      return
    }
    editing = {
      id: contact.id,
      bookId: contact.bookId,
      fullName: contact.fullName,
      organization: contact.organization,
      title: contact.title,
      note: contact.note,
      emails: contact.emails.length ? contact.emails.map((e) => ({ ...e })) : [{ value: '', label: '' }],
      phones: contact.phones.map((p) => ({ ...p })),
      matrixId: contact.matrixId,
      force: false,
    }
  }

  function openMail(message: MessageSummary): void {
    revealMessage(message.id, message.accountId, message.folderId, $sidebar.data ?? null)
    openSpace('mail')
  }

  function manageBooks(): void {
    openSettingsAt('contacts')
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape' && sideOpen) {
      event.stopPropagation()
      sideOpen = false
    }
  }

  // drawers slide in from their own edge, which is the other side in RTL.
  function edge(): number {
    return document.documentElement.dir === 'rtl' ? 1 : -1
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<section class="space" aria-label={$t('contacts.title')} bind:this={root} bind:clientWidth={width} on:keydown={onKeydown}>
  <header class="head">
    {#if mode !== 'wide' && $addressBooks.length > 0}
      <button
        type="button"
        class="icon"
        aria-label={$t('contacts.side.label')}
        aria-expanded={sideOpen}
        on:click={() => (sideOpen = !sideOpen)}
      >
        <Ph svg={sidebarIcon} size={18} />
      </button>
    {/if}
    <div class="title">
      <Ph svg={bookIcon} size={20} />
      <h2>{$t('contacts.title')}</h2>
    </div>
    <div class="actions">
      <div class="switch" role="group" aria-label={$t('contacts.view.label')}>
        <button
          type="button"
          class="seg"
          aria-pressed={view === 'panes'}
          aria-label={$t('contacts.view.panes')}
          title={$t('contacts.view.panes')}
          on:click={() => setContactsView('panes')}
        >
          <Ph svg={listIcon} size={16} />
        </button>
        <button
          type="button"
          class="seg"
          aria-pressed={view === 'grid'}
          aria-label={$t('contacts.view.grid')}
          title={$t('contacts.view.grid')}
          on:click={() => setContactsView('grid')}
        >
          <Ph svg={gridIcon} size={16} />
        </button>
      </div>
      <button type="button" class="btn" on:click={refreshContacts} disabled={$contactsLoading}>
        <span class="spin" class:on={$contactsLoading}><Ph svg={syncIcon} size={16} /></span>
        {#if mode !== 'narrow'}{$t('contacts.sync')}{/if}
      </button>
      <button type="button" class="btn primary" on:click={() => startCreate()} disabled={$addressBooks.length === 0}>
        <Ph svg={plusIcon} size={16} />
        {#if mode !== 'narrow'}{$t('contacts.new')}{/if}
      </button>
    </div>
  </header>

  {#if troubled.length > 0}
    <div class="problems">
      {#each troubled as book (book.id)}
        <p>
          {book.hasPassword
            ? $t('contacts.bookFailed').replace('{name}', book.name || $t('contacts.book.untitled'))
            : $t('contacts.bookNeedsPassword').replace('{name}', book.name || $t('contacts.book.untitled'))}
        </p>
      {/each}
    </div>
  {/if}

  {#if $addressBooks.length === 0 && ready}
    <div class="empty">
      <Ph svg={bookIcon} size={32} />
      <p>{$t('contacts.noBooks')}</p>
      <button type="button" class="btn primary" on:click={manageBooks}>{$t('contacts.addBook')}</button>
    </div>
  {:else}
    <div class="content {layout}">
      {#if mode === 'wide'}
        <ContactSidebar on:manage={manageBooks} />
      {/if}

      {#if view === 'grid'}
        <ContactGrid on:select={(e) => select(e.detail)} />
      {:else if mode !== 'narrow' || !paneOpen}
        <ContactList on:select={(e) => select(e.detail)} />
      {/if}

      {#if view === 'panes' && mode !== 'narrow'}
        <ContactPane
          contact={selected}
          draft={editing}
          on:compose={(e) => dispatch('compose', e.detail)}
          on:edit={(e) => startEdit(e.detail)}
          on:closeEditor={closeEditor}
          on:dirty={(e) => (dirty = e.detail)}
          on:openMail={(e) => openMail(e.detail)}
        />
      {:else if view === 'panes' && paneOpen}
        <ContactPane
          contact={selected}
          draft={editing}
          dismiss="back"
          on:compose={(e) => dispatch('compose', e.detail)}
          on:edit={(e) => startEdit(e.detail)}
          on:closeEditor={closeEditor}
          on:dirty={(e) => (dirty = e.detail)}
          on:dismiss={dismissPane}
          on:openMail={(e) => openMail(e.detail)}
        />
      {/if}

      {#if view === 'grid' && paneOpen}
        <div class="drawer end" class:full={mode === 'narrow'} transition:fly={{ x: -edge() * 24, duration: dur(motion.slow), easing: cubicOut }}>
          <ContactPane
            contact={selected}
            draft={editing}
            dismiss="close"
            on:compose={(e) => dispatch('compose', e.detail)}
            on:edit={(e) => startEdit(e.detail)}
            on:closeEditor={closeEditor}
            on:dirty={(e) => (dirty = e.detail)}
            on:dismiss={dismissPane}
            on:openMail={(e) => openMail(e.detail)}
          />
        </div>
      {/if}

      {#if sideOpen && mode !== 'wide'}
        <button
          type="button"
          class="scrim"
          tabindex="-1"
          aria-label={$t('action.close')}
          transition:fade={{ duration: dur(motion.base) }}
          on:click={() => (sideOpen = false)}
        ></button>
        <div class="drawer start" transition:fly={{ x: edge() * 24, duration: dur(motion.slow), easing: cubicOut }}>
          <ContactSidebar on:manage={manageBooks} on:pick={() => (sideOpen = false)} />
        </div>
      {/if}
    </div>
  {/if}
</section>

{#if $contactConflict}
  <ContactConflictDialog conflict={$contactConflict} />
{/if}

<style>
  .space {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    background: var(--surface-raised);
    color: var(--text-primary);
    font-family: var(--font-ui);
  }

  .head {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    border-bottom: var(--hairline) solid var(--border-default);
    background: var(--surface-raised);
  }

  .title {
    display: flex;
    flex: 1;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
    color: var(--text-secondary);
  }

  h2 {
    margin: 0;
    color: var(--text-primary);
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
  }

  .actions {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .switch {
    display: inline-flex;
    padding: 2px;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }

  .seg {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--control-height);
    height: calc(var(--control-height) - 6px);
    border: none;
    border-radius: calc(var(--radius-control) - 1px);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .seg:hover {
    color: var(--text-primary);
  }

  .seg[aria-pressed='true'] {
    background: var(--surface-raised);
    color: var(--text-primary);
    box-shadow: 0 0 0 var(--hairline) var(--border-default);
  }

  .btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    white-space: nowrap;
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .btn:hover:not(:disabled) {
    background: var(--surface-hover);
  }

  .btn:active:not(:disabled) {
    transform: scale(0.98);
  }

  .btn.primary {
    border-color: transparent;
    background: var(--accent);
    color: var(--accent-fg);
  }

  .btn.primary:hover:not(:disabled) {
    background: var(--accent);
    filter: brightness(0.94);
  }

  .btn:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .spin {
    display: inline-flex;
  }

  .spin.on {
    animation: spin 900ms linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
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

  .problems {
    padding: var(--space-2) var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
    background: var(--warning-bg);
    color: var(--warning);
    font-size: var(--fz-meta);
  }

  .problems p {
    margin: 0;
  }

  .content {
    position: relative;
    display: grid;
    flex: 1;
    min-height: 0;
    overflow: hidden;
  }

  .content > :global(:not(.drawer):not(.scrim) + :not(.drawer):not(.scrim)) {
    border-inline-start: var(--hairline) solid var(--border-default);
  }

  .panes-wide {
    grid-template-columns: 220px minmax(260px, 340px) minmax(0, 1fr);
  }

  .panes-medium {
    grid-template-columns: minmax(240px, 320px) minmax(0, 1fr);
  }

  .panes-narrow,
  .grid-medium,
  .grid-narrow {
    grid-template-columns: minmax(0, 1fr);
  }

  .grid-wide {
    grid-template-columns: 220px minmax(0, 1fr);
  }

  .scrim {
    position: absolute;
    inset: 0;
    z-index: 3;
    padding: 0;
    border: none;
    background: var(--scrim);
    cursor: default;
  }

  .drawer {
    position: absolute;
    inset-block: 0;
    z-index: 4;
    display: flex;
    flex-direction: column;
    width: 340px;
    max-width: 100%;
    background: var(--surface-raised);
    box-shadow: var(--shadow-overlay);
  }

  .drawer > :global(*) {
    flex: 1;
    min-height: 0;
  }

  .drawer.start {
    inset-inline-start: 0;
    border-inline-end: var(--hairline) solid var(--border-default);
  }

  .drawer.end {
    inset-inline-end: 0;
    width: 420px;
    border-inline-start: var(--hairline) solid var(--border-default);
  }

  .drawer.full {
    width: 100%;
  }

  .empty {
    display: flex;
    flex: 1;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    padding: var(--space-5);
    color: var(--text-tertiary);
    font-size: var(--fz-body);
    text-align: center;
  }

  .empty p {
    margin: 0;
    max-width: 360px;
  }

  @media (prefers-reduced-motion: reduce) {
    .seg,
    .btn {
      transition: none;
    }
  }
</style>
