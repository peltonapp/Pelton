<script lang="ts">
  // the key lookup log: each time Pelton asked the network for a key, when,
  // where and for which address, so the privacy cost of lookups stays visible.
  import { onDestroy, onMount } from 'svelte'
  import Card from './ui/Card.svelte'
  import { clearKeyLookupLog, listKeyLookupLog } from '../../lib/api'
  import { onAutoEncryptChanged } from '../../lib/events'
  import { errorMessage, toastError } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { KeyLookupLogEntry } from '../../lib/types'
  import '../../theme/controls.css'

  let entries: KeyLookupLogEntry[] = []
  let loaded = false

  async function load(): Promise<void> {
    try {
      entries = await listKeyLookupLog()
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      loaded = true
    }
  }

  onMount(() => void load())
  const stop = onAutoEncryptChanged(() => void load())
  onDestroy(stop)

  /** where names the host that was asked. */
  function where(e: KeyLookupLogEntry): string {
    if (e.source === 'wkd') {
      return $t('autoEncrypt.log.where.wkd').replace('{domain}', e.email.split('@')[1] ?? '')
    }
    if (e.source.startsWith('server:')) {
      return e.source.slice('server:'.length).replace(/^https:\/\//, '')
    }
    return e.source
  }

  async function clear(): Promise<void> {
    try {
      await clearKeyLookupLog()
      await load()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<Card id="key-lookup-log" title={$t('autoEncrypt.log.title')} description={$t('autoEncrypt.log.hint')}>
  {#if loaded && entries.length === 0}
    <p class="empty">{$t('autoEncrypt.log.empty')}</p>
  {:else}
    <table>
      <tbody>
        {#each entries as e, i (i)}
          <tr>
            <td class="when">{new Date(e.at).toLocaleString()}</td>
            <td>{where(e)}</td>
            <td class="addr">{e.email}</td>
            <td class="outcome {e.outcome}">{$t(`autoEncrypt.log.outcome.${e.outcome}`)}</td>
          </tr>
        {/each}
      </tbody>
    </table>
    <div class="actions">
      <button type="button" class="cal-btn" on:click={() => void clear()}>{$t('autoEncrypt.log.clear')}</button>
    </div>
  {/if}
</Card>

<style>
  .empty {
    margin: 0;
    padding: var(--space-3) var(--space-4);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  td {
    padding: var(--space-2) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .when {
    white-space: nowrap;
    color: var(--text-tertiary);
  }

  .addr {
    color: var(--text-primary);
    word-break: break-all;
  }

  .outcome.found {
    color: var(--success);
  }

  .outcome.failed {
    color: var(--warning);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    padding: var(--space-3) var(--space-4);
  }
</style>
