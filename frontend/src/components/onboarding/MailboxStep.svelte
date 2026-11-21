<script lang="ts">
  // the mailbox step of the setup: the add-mailbox wizard inline, with its
  // provider guidance, and importing from Thunderbird, mail files or a backup
  // as the other way in. mailboxes that already exist are listed instead, with
  // a way to add another.
  import { createEventDispatcher } from 'svelte'
  import Ph from '../common/Ph.svelte'
  import AddMailboxWizard from '../wizard/AddMailboxWizard.svelte'
  import importIcon from '@phosphor-icons/core/assets/duotone/tray-arrow-down-duotone.svg?raw'
  import arrowIcon from '@phosphor-icons/core/assets/duotone/arrow-right-duotone.svg?raw'
  import checkIcon from '@phosphor-icons/core/assets/duotone/check-circle-duotone.svg?raw'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import '../../theme/controls.css'
  import { refreshSidebar } from '../../stores/accounts'
  import { t } from '../../lib/i18n'
  import { loadImportDialog } from '../../lib/lazyload'
  import type { Account } from '../../lib/types'

  /** The mailboxes there are already. */
  export let accounts: Account[] = []

  const dispatch = createEventDispatcher<{ added: Account; done: Account }>()

  let adding = false
  let showImport = false

  function closeImport(): void {
    showImport = false
    void refreshSidebar()
  }
</script>

{#if accounts.length > 0 && !adding}
  <ul class="existing" aria-label={$t('setup.mailbox.existing')}>
    {#each accounts as a (a.id)}
      <li><Ph svg={checkIcon} size={16} /><span>{a.email}</span></li>
    {/each}
  </ul>
  <button type="button" class="cal-btn" on:click={() => (adding = true)}>
    <Ph svg={plusIcon} size={13} />{$t('setup.mailbox.another')}
  </button>
{:else}
  <AddMailboxWizard embedded offerImport={false} on:added={(e) => dispatch('added', e.detail)} on:done={(e) => dispatch('done', e.detail)} />
{/if}

<button type="button" class="import" on:click={() => (showImport = true)}>
  <Ph svg={importIcon} size={18} />
  <span class="import-text">
    <span class="import-title">{$t('wizard.start.importTitle')}</span>
    <span class="muted">{$t('import.entry.sub')}</span>
  </span>
  <Ph svg={arrowIcon} size={14} />
</button>

{#if showImport}
  {#await loadImportDialog() then m}
    <svelte:component this={m.default} on:close={closeImport} />
  {/await}
{/if}

<style>
  .existing {
    display: flex;
    flex-direction: column;
    margin: 0 0 var(--space-4);
    padding: 0;
    list-style: none;
    border-top: var(--hairline) solid var(--border-default);
  }

  .existing li {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3) 0;
    border-bottom: var(--hairline) solid var(--border-default);
    font-size: var(--fz-list);
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  .existing :global(.ph) {
    color: var(--success);
  }

  .import {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    width: 100%;
    margin-top: var(--space-6);
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    color: var(--text-secondary);
    text-align: start;
    cursor: var(--cursor-action);
    transition: border-color var(--motion-fast) var(--ease-out);
  }

  .import:hover {
    border-color: var(--border-strong);
  }

  .import:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .import-text {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-width: 0;
  }

  .import-title {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
  }

  .muted {
    font-size: var(--fz-label);
    line-height: 1.45;
    color: var(--text-tertiary);
  }
</style>
