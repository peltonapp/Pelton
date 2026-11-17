<script lang="ts">
  // the calendar and contacts step of the setup: one CalDAV and CardDAV login.
  // with a mailbox set up the server can be left empty, and the mail domain is
  // asked where its calendars and address books live. nothing is stored until
  // Add is pressed after the server answered.
  import { createEventDispatcher } from 'svelte'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import checkIcon from '@phosphor-icons/core/assets/duotone/check-circle-duotone.svg?raw'
  import '../../theme/controls.css'
  import { addAddressBook, addCalendarAccount, discoverAddressBooks, discoverCalendars } from '../../lib/api'
  import { checkDavServer } from '../../lib/setupplan'
  import { errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { Account, CalendarDiscovery, DiscoveredBook } from '../../lib/types'

  /** The mailbox discovery may start from, or null without one. */
  export let mailbox: Account | null = null
  /** How many logins were set up before this step. */
  export let existing = 0

  const dispatch = createEventDispatcher<{ added: number }>()

  let server = ''
  let username = mailbox?.email ?? ''
  let password = ''
  let problem = ''
  let error = ''
  let looking = false
  let saving = false
  let calendars: CalendarDiscovery | null = null
  let books: DiscoveredBook[] = []
  let calendarError = ''
  let bookError = ''
  let takeCalendars = true
  let takeBooks = new Set<string>()
  let added = 0
  let looked = false

  $: domain = mailbox ? mailbox.email.split('@')[1] ?? '' : ''
  $: google = /^(gmail|googlemail)\.com$/i.test(domain)
  // a mailbox added after this step was first shown fills the login once.
  let filledFrom = mailbox?.id ?? 0
  $: if (mailbox && mailbox.id !== filledFrom) {
    filledFrom = mailbox.id
    if (username === '') {
      username = mailbox.email
    }
  }
  $: fromMailbox = server.trim() === '' && mailbox !== null
  $: newBooks = books.filter((b) => !b.exists)
  $: canAdd = (takeCalendars && calendars !== null && calendars.calendars.length > 0 && !calendars.exists) || takeBooks.size > 0

  function reset(): void {
    calendars = null
    books = []
    calendarError = ''
    bookError = ''
    error = ''
    looked = false
  }

  function drafts(): { accountId: number; url: string } {
    return { accountId: fromMailbox && mailbox ? mailbox.id : 0, url: server.trim() }
  }

  async function look(): Promise<void> {
    const check = checkDavServer(server, mailbox !== null)
    problem = check ? $t(check) : ''
    if (check || looking) {
      return
    }
    looking = true
    reset()
    const { accountId, url } = drafts()
    const [cal, book] = await Promise.allSettled([
      discoverCalendars({ id: 0, accountId, addressBookId: 0, name: '', url, username: username.trim(), password }),
      discoverAddressBooks({ id: 0, accountId, name: '', url, collectionPath: '', username: username.trim(), password }),
    ])
    if (cal.status === 'fulfilled') {
      calendars = cal.value
    } else {
      calendarError = errorMessage(cal.reason)
    }
    if (book.status === 'fulfilled') {
      books = book.value
      takeBooks = new Set(book.value.filter((b) => !b.exists).map((b) => b.collectionPath))
    } else {
      bookError = errorMessage(book.reason)
    }
    looked = true
    looking = false
  }

  async function add(): Promise<void> {
    if (!canAdd || saving) {
      return
    }
    saving = true
    error = ''
    const { accountId } = drafts()
    let count = 0
    try {
      if (takeCalendars && calendars && calendars.calendars.length > 0 && !calendars.exists) {
        await addCalendarAccount({ id: 0, accountId, addressBookId: 0, name: '', url: calendars.url, username: calendars.username || username.trim(), password })
        count += 1
      }
      for (const book of newBooks.filter((b) => takeBooks.has(b.collectionPath))) {
        await addAddressBook({ id: 0, accountId, name: book.name, url: book.url, collectionPath: book.collectionPath, username: username.trim(), password })
        count += 1
      }
    } catch (err) {
      error = errorMessage(err)
    } finally {
      saving = false
    }
    if (count > 0) {
      added += count
      dispatch('added', count)
      server = ''
      password = ''
      reset()
    }
  }

  function toggleBook(path: string, on: boolean): void {
    const next = new Set(takeBooks)
    if (on) {
      next.add(path)
    } else {
      next.delete(path)
    }
    takeBooks = next
  }
</script>

{#if existing + added > 0}
  <p class="state"><Ph svg={checkIcon} size={16} />{$t('setup.dav.have').replace('{n}', String(existing + added))}</p>
{/if}

{#if google}
  <p class="note">{$t('setup.dav.google')}</p>
{/if}

<form class="form" on:submit|preventDefault={() => (looked && canAdd ? add() : look())}>
  <label>
    <span>{$t('setup.dav.server')}</span>
    <input
      class="cal-field"
      bind:value={server}
      placeholder={mailbox ? $t('setup.dav.serverFromMail').replace('{domain}', domain) : 'https://cloud.example.com/remote.php/dav'}
      aria-invalid={problem !== ''}
      aria-describedby={problem ? 'dav-problem' : undefined}
      on:input={() => {
        problem = ''
        reset()
      }}
    />
  </label>
  {#if problem}
    <p class="error" id="dav-problem"><Ph svg={warningIcon} size={13} />{problem}</p>
  {/if}
  <div class="pair">
    <label>
      <span>{$t('caldav.username')}</span>
      <input class="cal-field" bind:value={username} autocomplete="username" on:input={reset} />
    </label>
    <label>
      <span>{$t('caldav.password')}</span>
      <input class="cal-field" type="password" bind:value={password} autocomplete="current-password" on:input={reset} />
    </label>
  </div>
  <p class="muted">{$t('setup.dav.tasks')}</p>

  {#if looked}
    <div class="found" role="status">
      {#if calendars}
        <label class="pick">
          <input type="checkbox" bind:checked={takeCalendars} disabled={calendars.calendars.length === 0 || calendars.exists} />
          <span>
            {calendars.exists ? $t('caldav.exists') : $t('caldav.found').replace('{n}', String(calendars.calendars.length))}
          </span>
        </label>
      {:else if calendarError}
        <p class="error"><Ph svg={warningIcon} size={13} />{$t('setup.dav.noCalendars')} {calendarError}</p>
      {/if}
      {#each books as book (book.collectionPath)}
        <label class="pick">
          <input
            type="checkbox"
            checked={takeBooks.has(book.collectionPath)}
            disabled={book.exists}
            on:change={(e) => toggleBook(book.collectionPath, e.currentTarget.checked)}
          />
          <span>{$t('setup.dav.book').replace('{name}', book.name || book.collectionPath)}</span>
        </label>
      {/each}
      {#if bookError && books.length === 0}
        <p class="error"><Ph svg={warningIcon} size={13} />{$t('setup.dav.noBooks')} {bookError}</p>
      {/if}
    </div>
  {/if}
  {#if error}
    <p class="error" role="alert"><Ph svg={warningIcon} size={13} />{error}</p>
  {/if}

  <div class="actions">
    {#if looked && canAdd}
      <button type="submit" class="cal-btn primary" disabled={saving}>
        {#if saving}<PieProgress label={$t('caldav.saving')} />{/if}
        {$t('caldav.add')}
      </button>
    {:else}
      <button type="submit" class="cal-btn primary" disabled={looking}>
        {#if looking}<PieProgress label={$t('caldav.checking')} />{/if}
        {fromMailbox ? $t('setup.dav.lookAt').replace('{domain}', domain) : $t('setup.dav.look')}
      </button>
    {/if}
  </div>
</form>

<style>
  .form {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  label {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .pair {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(12em, 1fr));
    gap: var(--space-3);
  }

  .pick {
    flex-direction: row;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .pick input {
    accent-color: var(--accent);
  }

  .found {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  .state,
  .note,
  .error,
  .muted {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
    margin: 0;
    font-size: var(--fz-label);
    line-height: 1.45;
  }

  .state {
    margin-bottom: var(--space-4);
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .state :global(.ph) {
    color: var(--success);
  }

  .note {
    margin-bottom: var(--space-4);
    padding: var(--space-3) var(--space-4);
    border-inline-start: 2px solid var(--warning);
    background: var(--warning-bg);
    color: var(--text-secondary);
  }

  .error {
    color: var(--danger);
    overflow-wrap: anywhere;
  }

  .muted {
    color: var(--text-tertiary);
  }

  .actions {
    display: flex;
    gap: var(--space-2);
  }
</style>
