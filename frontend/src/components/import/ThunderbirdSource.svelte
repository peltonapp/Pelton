<script lang="ts">
  // moving in from Thunderbird (#196, #308): the profiles found where
  // Thunderbird keeps them, each with enough on it to tell them apart, then the
  // chosen profile's accounts and local mail with checkboxes, so what comes
  // across is a choice rather than all or nothing.
  //
  // Nothing here reads the network, and no password is read: Thunderbird keeps
  // its own, and each account is asked for its password once after it lands.
  import { createEventDispatcher, onMount } from 'svelte'
  import { get } from 'svelte/store'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import FlowButton from '../common/FlowButton.svelte'
  import backIcon from '@phosphor-icons/core/assets/duotone/arrow-left-duotone.svg?raw'
  import folderIcon from '@phosphor-icons/core/assets/duotone/folder-open-duotone.svg?raw'
  import { findThunderbirdProfiles, chooseThunderbirdProfile, importThunderbirdAccounts } from '../../lib/api'
  import { loadSidebar } from '../../stores/accounts'
  import { promptForMissingPasswords } from '../../stores/passwordprompt'
  import { toastError, toastSuccess, errorMessage } from '../../stores/toast'
  import { formatBytes } from '../../lib/format'
  import type { ThunderbirdProfile } from '../../lib/types'
  import type { RunItem } from './ImportRun.svelte'
  import { t } from '../../lib/i18n'

  /** True while a mail import runs, which locks the mailbox choice. */
  export let running = false

  const dispatch = createEventDispatcher<{ importFolders: RunItem[] }>()

  let profiles: ThunderbirdProfile[] = []
  let scanning = true
  let chosen: ThunderbirdProfile | null = null
  let addingAccounts = false

  // keyed by address and by path so a step back and forward keeps the ticks.
  let pickedAccounts = new Set<string>()
  let pickedFolders = new Set<string>()

  // a pop3 account cannot be recreated (Pelton speaks imap), and one already
  // configured would be a duplicate. Both are listed with the reason rather
  // than hidden: a missing account looks like Pelton failed to see it.
  $: accounts = chosen?.accounts ?? []
  $: importable = accounts.filter((a) => a.kind === 'imap' && !a.exists)
  $: folders = chosen?.localFolders ?? []
  $: pickedBytes = folders.filter((f) => pickedFolders.has(f.path)).reduce((sum, f) => sum + f.sizeBytes, 0)

  onMount(async () => {
    try {
      profiles = await findThunderbirdProfiles()
      // one profile is not a choice, so it opens straight into its contents.
      if (profiles.length === 1) {
        chosen = profiles[0]
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      scanning = false
    }
  })

  function profileSummary(p: ThunderbirdProfile): string {
    const accountCount = p.accounts.length
    const folderCount = p.localFolders.length
    const bytes = p.localFolders.reduce((sum, f) => sum + f.sizeBytes, 0)
    const parts = [
      accountCount === 1 ? $t('import.tb.oneAccount') : $t('import.tb.nAccounts').replace('{n}', String(accountCount)),
      folderCount === 1 ? $t('import.tb.oneMailbox') : $t('import.tb.nMailboxes').replace('{n}', String(folderCount)),
    ]
    if (bytes > 0) {
      parts.push(formatBytes(bytes))
    }
    return parts.join(' · ')
  }

  // the addresses on a profile card, capped so a profile with a dozen accounts
  // does not push the next one out of view.
  function addressLine(p: ThunderbirdProfile): string {
    const shown = p.accounts.slice(0, 3).map((a) => a.email)
    if (p.accounts.length > shown.length) {
      shown.push($t('import.tb.andMore').replace('{n}', String(p.accounts.length - shown.length)))
    }
    return shown.join(', ')
  }

  async function pickProfile(): Promise<void> {
    try {
      const found = await chooseThunderbirdProfile()
      if (found.length === 0) {
        return
      }
      profiles = [...found, ...profiles]
      chosen = found[0]
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function toggle(set: Set<string>, key: string): Set<string> {
    const next = new Set(set)
    if (next.has(key)) {
      next.delete(key)
    } else {
      next.add(key)
    }
    return next
  }

  function toggleAllAccounts(): void {
    pickedAccounts = pickedAccounts.size === importable.length ? new Set() : new Set(importable.map((a) => a.email))
  }

  function toggleAllFolders(): void {
    pickedFolders = pickedFolders.size === folders.length ? new Set() : new Set(folders.map((f) => f.path))
  }

  async function addAccounts(): Promise<void> {
    if (!chosen || pickedAccounts.size === 0) {
      return
    }
    addingAccounts = true
    try {
      const created = await importThunderbirdAccounts(chosen.path, [...pickedAccounts])
      pickedAccounts = new Set()
      // re-reading marks the imported ones as existing, so they are not
      // offered a second time.
      profiles = await findThunderbirdProfiles()
      chosen = profiles.find((p) => p.path === chosen?.path) ?? chosen
      await loadSidebar()
      toastSuccess(`${created} ${get(t)('import.accountsAdded')}`)
      // an imported mailbox arrives without its password and cannot sync until
      // it has one, so it is asked for while the user is still setting up.
      if (created > 0) {
        await promptForMissingPasswords()
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      addingAccounts = false
    }
  }

  function importFolders(): void {
    const items = folders.filter((f) => pickedFolders.has(f.path)).map((f) => ({ key: f.path, label: f.name }))
    if (items.length === 0) {
      return
    }
    pickedFolders = new Set()
    dispatch('importFolders', items)
  }
</script>

<div class="source">
  {#if scanning}
    <p class="status"><PieProgress size={14} label={$t('import.scanning')} /> {$t('import.scanning')}</p>
  {:else if !chosen}
    {#if profiles.length === 0}
      <p class="hint">{$t('import.tb.none')}</p>
    {:else}
      <p class="hint">{$t('import.tb.intro')}</p>
      <ul class="profiles">
        {#each profiles as p (p.path)}
          <li>
            <button type="button" class="profile" on:click={() => (chosen = p)}>
              <span class="p-name">{p.name}</span>
              <span class="p-summary">{profileSummary(p)}</span>
              {#if p.accounts.length > 0}
                <span class="p-addresses">{addressLine(p)}</span>
              {/if}
            </button>
          </li>
        {/each}
      </ul>
    {/if}
    <button type="button" class="link" on:click={pickProfile}>
      <Ph svg={folderIcon} size={14} />
      {$t('import.tb.choose')}
    </button>
  {:else}
    <div class="crumb">
      <button type="button" class="link back" disabled={running} on:click={() => (chosen = null)}>
        <Ph svg={backIcon} size={14} />
        {$t('import.tb.backToProfiles')}
      </button>
      <span class="profile-name">{chosen.name}</span>
    </div>

    <section class="block">
      <header class="block-head">
        <h4>{$t('import.tb.accounts')}</h4>
        {#if importable.length > 0}
          <button type="button" class="link" on:click={toggleAllAccounts}>
            {pickedAccounts.size === importable.length ? $t('import.tb.none_') : $t('import.tb.all')}
          </button>
        {/if}
      </header>
      {#if accounts.length === 0}
        <p class="hint">{$t('import.tb.noAccounts')}</p>
      {:else}
        <div class="rows">
          {#each accounts as account (account.email)}
            {@const blocked = account.kind !== 'imap' || account.exists}
            <label class="row" class:blocked>
              <input
                type="checkbox"
                checked={pickedAccounts.has(account.email)}
                disabled={blocked || addingAccounts}
                on:change={() => (pickedAccounts = toggle(pickedAccounts, account.email))}
              />
              <span class="row-main">
                <span class="row-title">{account.email}</span>
                <span class="row-meta">
                  {#if account.exists}
                    {$t('import.tb.alreadyHere')}
                  {:else if account.kind !== 'imap'}
                    {$t('import.tb.pop3').replace('{kind}', account.kind.toUpperCase())}
                  {:else}
                    {account.imapHost}
                  {/if}
                </span>
              </span>
            </label>
          {/each}
        </div>
        <div class="block-foot">
          <p class="hint">{$t('import.tb.passwordNote')}</p>
          <FlowButton disabled={addingAccounts || pickedAccounts.size === 0} on:click={addAccounts}>
            {#if addingAccounts}<PieProgress size={12} label={$t('import.importing')} />{/if}
            {pickedAccounts.size === 1 ? $t('import.tb.addOne') : $t('import.tb.addN').replace('{n}', String(pickedAccounts.size))}
          </FlowButton>
        </div>
      {/if}
    </section>

    <section class="block">
      <header class="block-head">
        <h4>{$t('import.tb.mailboxes')}</h4>
        {#if folders.length > 0}
          <button type="button" class="link" disabled={running} on:click={toggleAllFolders}>
            {pickedFolders.size === folders.length ? $t('import.tb.none_') : $t('import.tb.all')}
          </button>
        {/if}
      </header>
      {#if folders.length === 0}
        <p class="hint">{$t('import.tb.noMailboxes')}</p>
      {:else}
        <div class="rows scroll">
          {#each folders as folder (folder.path)}
            <label class="row">
              <input
                type="checkbox"
                checked={pickedFolders.has(folder.path)}
                disabled={running}
                on:change={() => (pickedFolders = toggle(pickedFolders, folder.path))}
              />
              <span class="row-main">
                <span class="row-title">{folder.name}</span>
              </span>
              <span class="size">{formatBytes(folder.sizeBytes)}</span>
            </label>
          {/each}
        </div>
        <div class="block-foot end">
          <FlowButton variant="primary" disabled={running || pickedFolders.size === 0} on:click={importFolders}>
            {pickedFolders.size === 1
              ? $t('import.tb.importOne')
              : $t('import.tb.importN').replace('{n}', String(pickedFolders.size)).replace('{size}', formatBytes(pickedBytes))}
          </FlowButton>
        </div>
      {/if}
    </section>
  {/if}
</div>

<style>
  .source {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .status {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .hint {
    margin: 0;
    font-size: var(--fz-label);
    line-height: 1.5;
    color: var(--text-tertiary);
  }

  .profiles {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .profile {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    width: 100%;
    padding: var(--space-3) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    text-align: start;
    cursor: var(--cursor-action);
    transition:
      border-color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .profile:hover {
    border-color: var(--accent);
  }

  .profile:active {
    transform: scale(0.99);
  }

  .p-name {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
  }

  .p-summary {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .p-addresses {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .crumb {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
  }

  .profile-name {
    min-width: 0;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .block {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .block-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-3);
  }

  h4 {
    margin: 0;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  .rows {
    display: flex;
    flex-direction: column;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
  }

  .rows.scroll {
    max-height: 14rem;
    overflow-y: auto;
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    cursor: var(--cursor-action);
  }

  .row + .row {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .row input {
    accent-color: var(--accent);
  }

  .row.blocked {
    opacity: 0.55;
    cursor: default;
  }

  .row-main {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-width: 0;
  }

  .row-title {
    font-size: var(--fz-label);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row-meta,
  .size {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .size {
    flex-shrink: 0;
    font-family: var(--font-mono);
  }

  .block-foot {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }

  .block-foot.end {
    justify-content: flex-end;
  }

  .block-foot .hint {
    flex: 1;
    min-width: 12em;
  }

  .link {
    display: inline-flex;
    align-items: center;
    align-self: flex-start;
    gap: var(--space-1);
    padding: 0;
    border: none;
    background: transparent;
    color: var(--link);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .link:disabled {
    color: var(--text-tertiary);
    cursor: default;
  }

  .link.back {
    color: var(--text-secondary);
  }
</style>
