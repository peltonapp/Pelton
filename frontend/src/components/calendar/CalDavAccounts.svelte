<script lang="ts">
  // the CalDAV logins the calendars sync through, for the Calendar settings
  // page: each with when it last synced or why it failed, and a form to add
  // one. adding asks the server which calendars the login sees first, so a
  // wrong address or password is said before anything is stored.
  import { onMount, tick } from 'svelte'
  import { slide } from 'svelte/transition'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import cloudIcon from '@phosphor-icons/core/assets/duotone/cloud-check-duotone.svg?raw'
  import '../../theme/controls.css'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import { calendarAccounts, loadCalendars, refreshCalendars, calendarSync } from '../../stores/calendar'
  import { addCalendarAccount, discoverCalendars, removeCalendarAccount, updateCalendarAccount } from '../../lib/api'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { formatRelative } from '../../lib/format'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { CalendarAccount, CalendarAccountDraft, CalendarDiscovery } from '../../lib/types'

  const blank: CalendarAccountDraft = { id: 0, accountId: 0, addressBookId: 0, name: '', url: '', username: '', password: '' }

  let form: CalendarAccountDraft | null = null
  let found: CalendarDiscovery | null = null
  let working = false
  let error = ''
  let confirmRemove = 0
  let urlInput: HTMLInputElement

  onMount(loadCalendars)

  async function open(account: CalendarAccount | null): Promise<void> {
    form = account
      ? { ...blank, id: account.id, name: account.name, url: account.url, username: account.username }
      : { ...blank }
    found = null
    error = ''
    await tick()
    urlInput?.focus()
  }

  function close(): void {
    form = null
    found = null
    error = ''
  }

  async function check(): Promise<void> {
    if (!form || working) {
      return
    }
    working = true
    error = ''
    try {
      found = await discoverCalendars(form)
      form = { ...form, url: found.url || form.url, username: found.username || form.username }
      if (found.exists && form.id === 0) {
        error = $t('caldav.exists')
      }
    } catch (err) {
      found = null
      error = errorMessage(err)
    } finally {
      working = false
    }
  }

  async function save(): Promise<void> {
    if (!form || working) {
      return
    }
    working = true
    error = ''
    try {
      if (form.id === 0) {
        await addCalendarAccount(form)
        toastSuccess($t('caldav.added'))
      } else {
        await updateCalendarAccount(form)
        toastSuccess($t('caldav.updated'))
      }
      close()
      await loadCalendars()
    } catch (err) {
      error = errorMessage(err)
    } finally {
      working = false
    }
  }

  async function remove(id: number): Promise<void> {
    confirmRemove = 0
    try {
      await removeCalendarAccount(id)
      await loadCalendars()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function status(account: CalendarAccount): string {
    if (account.lastError) {
      return account.lastError
    }
    if (!account.lastSync) {
      return $t('caldav.neverSynced')
    }
    return $t('caldav.synced').replace('{when}', formatRelative(new Date(account.lastSync).getTime(), $t))
  }

  $: syncing = $calendarSync !== null
</script>

<div class="accounts">
  {#if $calendarAccounts.length === 0 && !form}
    <EmptyState size="compact" kind="setup" title={$t('caldav.noneTitle')} detail={$t('caldav.none')} />
  {/if}
  {#each $calendarAccounts as account (account.id)}
    <div class="account">
      <span class="icon" class:bad={account.lastError !== ''}>
        <Ph svg={account.lastError ? warningIcon : cloudIcon} size={16} />
      </span>
      <div class="text">
        <span class="name">{account.name || account.username}</span>
        <span class="meta" title={account.url}>
          {$t('caldav.calendars').replace('{n}', String(account.calendarCount))} · {status(account)}
        </span>
      </div>
      {#if confirmRemove === account.id}
        <span class="confirm">{$t('caldav.removeConfirm')}</span>
        <button type="button" class="cal-btn" on:click={() => (confirmRemove = 0)}>{$t('calendar.cancel')}</button>
        <button type="button" class="cal-btn danger" on:click={() => remove(account.id)}>{$t('caldav.remove')}</button>
      {:else}
        <button type="button" class="cal-btn" on:click={() => open(account)}>{$t('calendar.edit')}</button>
        <button type="button" class="cal-btn danger" on:click={() => (confirmRemove = account.id)}>{$t('caldav.remove')}</button>
      {/if}
    </div>
  {/each}

  {#if form}
    <form class="form" on:submit|preventDefault={() => (found || form?.id ? save() : check())} transition:slide={{ duration: dur(motion.base) }}>
      <label>
        <span>{$t('caldav.url')}</span>
        <input class="cal-field" bind:this={urlInput} bind:value={form.url} placeholder="https://cloud.example.com/remote.php/dav" on:input={() => (found = null)} />
      </label>
      <div class="pair">
        <label>
          <span>{$t('caldav.username')}</span>
          <input class="cal-field" bind:value={form.username} autocomplete="username" on:input={() => (found = null)} />
        </label>
        <label>
          <span>{$t('caldav.password')}</span>
          <input
            class="cal-field"
            type="password"
            bind:value={form.password}
            autocomplete="current-password"
            placeholder={form.id ? $t('caldav.passwordKeep') : ''}
            on:input={() => (found = null)}
          />
        </label>
      </div>
      <label>
        <span>{$t('caldav.name')}</span>
        <input class="cal-field" bind:value={form.name} placeholder={$t('caldav.namePlaceholder')} />
      </label>

      {#if found}
        <div class="found" transition:slide={{ duration: dur(motion.base) }}>
          <p>{$t('caldav.found').replace('{n}', String(found.calendars.length))}</p>
          <ul>
            {#each found.calendars as c (c.path)}
              <li>
                <span class="dot" style={`--c:${c.color || 'var(--cal-1)'}`}></span>
                {c.name || c.path}
                {#if c.readOnly}<span class="ro">{$t('calendar.readOnly')}</span>{/if}
              </li>
            {/each}
          </ul>
        </div>
      {/if}
      {#if error}
        <p class="error"><Ph svg={warningIcon} size={13} />{error}</p>
      {/if}

      <div class="actions">
        <button type="button" class="cal-btn" on:click={close}>{$t('calendar.cancel')}</button>
        {#if found || form.id}
          <button type="submit" class="cal-btn primary" disabled={working || (found?.exists && form.id === 0)}>
            {#if working}<PieProgress label={$t('caldav.saving')} />{/if}
            {form.id ? $t('calendar.save') : $t('caldav.add')}
          </button>
        {:else}
          <button type="submit" class="cal-btn primary" disabled={working || !form.url.trim()}>
            {#if working}<PieProgress label={$t('caldav.checking')} />{/if}
            {$t('caldav.check')}
          </button>
        {/if}
      </div>
    </form>
  {:else}
    <div class="foot">
      <button type="button" class="cal-btn" on:click={() => open(null)}>
        <Ph svg={plusIcon} size={13} />{$t('caldav.addAccount')}
      </button>
      {#if $calendarAccounts.length > 0}
        <button type="button" class="cal-btn" disabled={syncing} on:click={() => void refreshCalendars()}>
          {#if syncing}<PieProgress label={$t('calendar.syncing')} />{/if}
          {$t('caldav.syncNow')}
        </button>
      {/if}
    </div>
  {/if}
</div>

<style>
  .accounts {
    display: flex;
    flex-direction: column;
  }

  .account {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .icon {
    display: inline-flex;
    color: var(--success);
  }

  .icon.bad {
    color: var(--warning);
  }

  .text {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    flex: 1;
    min-width: 0;
  }

  .name {
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .meta {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .confirm {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .form {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-4);
  }

  label {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    flex: 1;
    min-width: 0;
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .pair {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  .found {
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-size: var(--fz-label);
  }

  .found p {
    margin: 0 0 var(--space-2);
    color: var(--text-secondary);
  }

  .found ul {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .found li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--text-primary);
  }

  .dot {
    width: var(--space-3);
    height: var(--space-3);
    border-radius: 50%;
    background: var(--c);
  }

  .ro {
    color: var(--text-tertiary);
  }

  .error {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
    font-size: var(--fz-label);
    color: var(--danger);
  }

  .actions,
  .foot {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
  }

  .foot {
    justify-content: flex-start;
    padding: var(--space-3) var(--space-4);
  }
</style>
