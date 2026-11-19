<script lang="ts">
  // the top of the out of office and server filter pages: which mailbox, how
  // its filter service (ManageSieve) is reached, what state it is in, and the
  // undo of the last save. the page below it edits `state` once it is ok.
  import { onMount } from 'svelte'
  import { slide } from 'svelte/transition'
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import Segmented from '../ui/Segmented.svelte'
  import Select from '../../common/Select.svelte'
  import PieProgress from '../../common/PieProgress.svelte'
  import { listAccounts, sieveState, recheckSieve, setSieveConnection, restoreSievePrevious } from '../../../lib/api'
  import { accountLabel } from '../../../lib/format'
  import { dur, motion } from '../../../lib/motion'
  import { askConfirm } from '../../../stores/confirm'
  import { toastError, toastSuccess, errorMessage } from '../../../stores/toast'
  import { t } from '../../../lib/i18n'
  import type { Account, SieveState } from '../../../lib/types'

  /** The chosen mailbox's state, null until it has loaded. */
  export let state: SieveState | null = null
  /** True while a call to the server is running. */
  export let busy: boolean = false
  /** Anchor id prefix for settings search. */
  export let anchor: string

  let accounts: Account[] = []
  let accountId = 0
  let loaded = false
  let showConnection = false
  let host = ''
  let port = ''
  let security = 'starttls'

  onMount(async () => {
    try {
      accounts = (await listAccounts()).filter((a) => !a.local)
    } catch (err) {
      toastError(errorMessage(err))
    }
    loaded = true
    if (accounts.length > 0) {
      await pick(accounts[0].id)
    }
  })

  $: accountItems = accounts.map((a) => ({ value: String(a.id), label: accountLabel(a) }))
  $: account = accounts.find((a) => a.id === accountId) ?? null

  async function run(call: () => Promise<SieveState>): Promise<void> {
    busy = true
    try {
      state = await call()
      syncConnectionFields(state)
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  function syncConnectionFields(s: SieveState): void {
    host = s.manual ? s.host : ''
    port = s.manual && s.port ? String(s.port) : ''
    security = s.tls === 'ssl' ? 'ssl' : 'starttls'
    if (s.status !== 'ok' && s.status !== 'credentials' && s.status !== 'auth') {
      showConnection = true
    }
  }

  async function pick(id: number): Promise<void> {
    accountId = id
    state = null
    showConnection = false
    await run(() => sieveState(id))
  }

  function connect(): void {
    const n = port.trim() === '' ? 0 : Number(port)
    if (!Number.isInteger(n) || n < 0 || n > 65535) {
      toastError($t('sieve.connection.badPort'))
      return
    }
    void run(() => setSieveConnection(accountId, host.trim(), n, security))
  }

  function useDefaults(): void {
    void run(() => setSieveConnection(accountId, '', 0, 'starttls'))
  }

  async function restore(): Promise<void> {
    const ok = await askConfirm({
      title: $t('sieve.restore.confirm'),
      body: $t('sieve.restore.body'),
      confirmLabel: $t('sieve.restore'),
    })
    if (!ok) {
      return
    }
    await run(() => restoreSievePrevious(accountId))
    if (state?.status === 'ok') {
      toastSuccess($t('sieve.restored'))
    }
  }

  $: securityOptions = [
    { key: 'starttls', label: $t('sieve.connection.starttls') },
    { key: 'ssl', label: $t('sieve.connection.ssl') },
  ]

  $: statusLine = !state
    ? $t('sieve.loading')
    : state.status === 'ok'
      ? $t('sieve.status.ok').replace('{host}', `${state.host}:${state.port}`)
      : $t(`sieve.status.${state.status}`)
</script>

<Card title={$t('sieve.mailbox')} id={`${anchor}-mailbox`}>
  {#if loaded && accounts.length === 0}
    <p class="empty">{$t('sieve.noServerAccounts')}</p>
  {:else}
    <Row label={account ? accountLabel(account) : ''} hint={account && accountLabel(account) !== account.email ? account.email : ''}>
      {#if accounts.length > 1}
        <Select
          value={String(accountId)}
          items={accountItems}
          ariaLabel={$t('sieve.mailbox')}
          disabled={busy}
          on:change={(e) => pick(Number(e.detail))}
        />
      {/if}
    </Row>
    <div class="status" aria-live="polite">
      {#if busy}
        <PieProgress label={$t('sieve.working')} />
      {/if}
      <div class="status-text">
        <p class="line">{statusLine}</p>
        {#if state && state.status === 'unsupported'}
          <p class="sv-note">{$t('sieve.status.unsupportedHint')}</p>
        {/if}
        {#if state && state.status !== 'ok' && state.detail}
          <p class="detail">{state.detail}</p>
        {/if}
      </div>
      <div class="status-actions">
        {#if state && state.status !== 'ok'}
          <button type="button" class="s2-btn" disabled={busy} on:click={() => run(() => recheckSieve(accountId))}>
            {$t('sieve.checkAgain')}
          </button>
        {/if}
        {#if state && state.hasPrevious && state.status === 'ok'}
          <button type="button" class="s2-btn" disabled={busy} on:click={restore}>{$t('sieve.restore')}</button>
        {/if}
        {#if state}
          <button
            type="button"
            class="s2-btn quiet"
            aria-expanded={showConnection}
            on:click={() => (showConnection = !showConnection)}
          >
            {$t('sieve.connection')}
          </button>
        {/if}
      </div>
    </div>
    {#if showConnection && state}
      <div class="connection" transition:slide|local={{ duration: dur(motion.base) }}>
        <label class="field">
          <span>{$t('sieve.connection.host')}</span>
          <input class="sv-input" type="text" bind:value={host} placeholder={account?.imapHost ?? ''} spellcheck="false" />
        </label>
        <label class="field narrow">
          <span>{$t('sieve.connection.port')}</span>
          <input class="sv-input" type="text" inputmode="numeric" bind:value={port} placeholder="4190" />
        </label>
        <div class="field">
          <span>{$t('sieve.connection.security')}</span>
          <Segmented label={$t('sieve.connection.security')} value={security} options={securityOptions} on:change={(e) => (security = e.detail)} />
        </div>
        <div class="connection-actions">
          {#if state.manual}
            <button type="button" class="s2-btn quiet" disabled={busy} on:click={useDefaults}>{$t('sieve.connection.reset')}</button>
          {/if}
          <button type="button" class="s2-btn" disabled={busy} on:click={connect}>{$t('sieve.connection.save')}</button>
        </div>
      </div>
    {/if}
    <p class="server-note">{$t('sieve.serverNote')}</p>
  {/if}
</Card>

<style>
  .empty {
    margin: 0;
    padding: var(--space-4);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .status {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    color: var(--text-secondary);
  }

  .status :global(.pie) {
    margin-top: var(--space-1);
  }

  .status-text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .line {
    margin: 0;
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .detail {
    margin: 0;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    overflow-wrap: anywhere;
    user-select: text;
    -webkit-user-select: text;
  }

  .status-actions,
  .connection-actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    flex-shrink: 0;
  }

  .connection {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    flex: 1 1 12em;
    min-width: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .field.narrow {
    flex: 0 0 6em;
  }

  .connection-actions {
    margin-inline-start: auto;
  }

  .server-note {
    margin: 0;
    padding: var(--space-2) var(--space-4) var(--space-3);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
