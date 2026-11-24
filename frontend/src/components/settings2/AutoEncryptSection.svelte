<script lang="ts">
  // automatic encryption in the new settings: the switch, which starts the
  // setup walk through; where keys may come from; hiding the subject; each
  // mailbox's own key with its backup; other people's keys and the lookup log.
  import { onDestroy, onMount } from 'svelte'
  import Card from './ui/Card.svelte'
  import Row from './ui/Row.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import Modal from '../common/Modal.svelte'
  import Ph from '../common/Ph.svelte'
  import globeIcon from '@phosphor-icons/core/assets/duotone/globe-duotone.svg?raw'
  import houseIcon from '@phosphor-icons/core/assets/duotone/house-duotone.svg?raw'
  import AutoEncryptSetup from './AutoEncryptSetup.svelte'
  import KeyServerList from './KeyServerList.svelte'
  import ContactKeysCard from './ContactKeysCard.svelte'
  import LookupLogCard from './LookupLogCard.svelte'
  import {
    backupOwnKey,
    getAutoEncryptSettings,
    sendKeyToOtherDevice,
    setAutoEncryptAccountMode,
    setKeySources,
    setProtectSubject,
  } from '../../lib/api'
  import { onAutoEncryptChanged } from '../../lib/events'
  import { groupFingerprint } from '../../lib/autoencrypt'
  import { prefs, setAutoEncrypt } from '../../stores/prefs'
  import { errorMessage, toastError, toastInfo, toastSuccess } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { AutoEncryptSettings, KeySources, OwnKey } from '../../lib/types'
  import '../../theme/controls.css'

  type SourceKey = 'autocrypt' | 'wkd' | 'keysOpenpgpOrg' | 'keyServers'
  const sourceRows: { key: SourceKey; name: string; local: boolean }[] = [
    { key: 'autocrypt', name: 'autocrypt', local: true },
    { key: 'wkd', name: 'wkd', local: false },
    { key: 'keysOpenpgpOrg', name: 'koo', local: false },
    { key: 'keyServers', name: 'servers', local: false },
  ]

  let settings: AutoEncryptSettings | null = null
  let setupOpen = false
  let backupFor: OwnKey | null = null
  let backupPass = ''
  let transferFor: OwnKey | null = null
  let transferCode = ''
  let busy = false

  $: enabled = $prefs.autoEncrypt

  async function load(): Promise<void> {
    try {
      settings = await getAutoEncryptSettings()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  onMount(() => void load())
  const stop = onAutoEncryptChanged(() => void load())
  onDestroy(stop)

  async function onSwitch(on: boolean): Promise<void> {
    if (on) {
      setupOpen = true
      return
    }
    try {
      await setAutoEncrypt(false)
      toastInfo($t('autoEncrypt.settings.offDone'))
      await load()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function finishSetup(sources: KeySources): Promise<void> {
    setupOpen = false
    try {
      await setAutoEncrypt(true, sources)
      await load()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function saveSources(next: KeySources): Promise<void> {
    try {
      await setKeySources(next)
      await load()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function setSource(key: SourceKey, on: boolean): void {
    if (settings) {
      void saveSources({ ...settings.sources, [key]: on })
    }
  }

  async function onSubject(on: boolean): Promise<void> {
    try {
      await setProtectSubject(on)
      await load()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function onMode(key: OwnKey, auto: boolean): Promise<void> {
    try {
      await setAutoEncryptAccountMode(key.accountId, auto ? 'auto' : 'manual')
      await load()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function saveBackup(): Promise<void> {
    if (!backupFor || backupPass.length < 8) {
      return
    }
    busy = true
    try {
      const path = await backupOwnKey(backupFor.accountId, backupPass)
      if (path) {
        toastSuccess($t('autoEncrypt.backup.saved').replace('{path}', path))
        backupFor = null
        backupPass = ''
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  async function sendTransfer(): Promise<void> {
    if (!transferFor) {
      return
    }
    busy = true
    try {
      transferCode = await sendKeyToOtherDevice(transferFor.accountId)
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  function closeTransfer(): void {
    transferFor = null
    transferCode = ''
  }

  function date(iso: string): string {
    return new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
  }

  function validity(key: OwnKey): string {
    if (!key.expires) {
      return $t('autoEncrypt.own.noExpiry')
    }
    return $t(key.generated ? 'autoEncrypt.own.expires' : 'autoEncrypt.own.expiresImported').replace('{date}', date(key.expires))
  }
</script>

<Card title={$t('autoEncrypt.settings.title')}>
  <Row id="auto-encrypt" label={$t('autoEncrypt.settings.switch')} hint={$t('autoEncrypt.settings.hint')}>
    <ToggleSwitch checked={enabled} label={$t('autoEncrypt.settings.switch')} on:change={(e) => void onSwitch(e.detail)} />
  </Row>
  {#if enabled && settings}
    <Row id="auto-encrypt-subject" label={$t('autoEncrypt.subject')} hint={$t('autoEncrypt.subject.hint')}>
      <ToggleSwitch checked={settings.protectSubject} label={$t('autoEncrypt.subject')} on:change={(e) => void onSubject(e.detail)} />
    </Row>
  {/if}
</Card>

{#if enabled && settings}
  <Card id="key-sources" title={$t('autoEncrypt.settings.sources')} description={$t('autoEncrypt.settings.sourcesHint')}>
    {#each sourceRows as row (row.key)}
      <Row stacked={row.key === 'keyServers' && settings.sources.keyServers} label={$t(`autoEncrypt.sourceName.${row.name}`)} hint={$t(`autoEncrypt.sourceExplain.${row.name}`)}>
        <span class="where">
          <Ph svg={row.local ? houseIcon : globeIcon} size={13} />
          <span>{row.local ? $t('autoEncrypt.source.local') : $t('autoEncrypt.source.network')}</span>
        </span>
        <ToggleSwitch checked={settings.sources[row.key]} label={$t(`autoEncrypt.sourceName.${row.name}`)} on:change={(e) => setSource(row.key, e.detail)} />
        {#if row.key === 'keyServers' && settings.sources.keyServers}
          <div class="servers">
            <KeyServerList servers={settings.sources.servers} on:change={(e) => settings && void saveSources({ ...settings.sources, servers: e.detail })} />
          </div>
        {/if}
      </Row>
    {/each}
  </Card>

  <Card id="own-keys" title={$t('autoEncrypt.own.title')} description={$t('autoEncrypt.own.hint')}>
    {#each settings.accounts as key (key.accountId)}
      <div class="own">
        <div class="own-head">
          <span class="email">{key.email}</span>
          {#if key.fingerprint}
            <code class="fp">{groupFingerprint(key.fingerprint)}</code>
            <span class="meta">{key.generated ? validity(key) : `${$t('autoEncrypt.own.imported')}. ${validity(key)}`}</span>
            {#if !key.usable}
              <span class="meta warn">{$t('autoEncrypt.own.unusable')}</span>
            {/if}
          {:else}
            <span class="meta">{$t('autoEncrypt.own.making')}</span>
          {/if}
        </div>
        <Row label={$t('autoEncrypt.own.mode')} hint={$t('autoEncrypt.own.modeHint')}>
          <ToggleSwitch
            checked={key.mode === 'auto'}
            label={$t('autoEncrypt.own.modeLabel').replace('{email}', key.email)}
            on:change={(e) => void onMode(key, e.detail)}
          />
        </Row>
        {#if key.fingerprint && key.usable}
          <div class="own-actions">
            <button type="button" class="cal-btn" aria-label={$t('autoEncrypt.own.backupLabel').replace('{email}', key.email)} on:click={() => (backupFor = key)}>
              {$t('autoEncrypt.own.backup')}
            </button>
            <button type="button" class="cal-btn" aria-label={$t('autoEncrypt.own.transferLabel').replace('{email}', key.email)} on:click={() => (transferFor = key)}>
              {$t('autoEncrypt.own.transfer')}
            </button>
          </div>
        {/if}
      </div>
    {/each}
  </Card>

  <ContactKeysCard />
  <LookupLogCard />
{/if}

{#if setupOpen}
  <AutoEncryptSetup on:close={() => (setupOpen = false)} on:done={(e) => void finishSetup(e.detail)} />
{/if}

{#if backupFor}
  <Modal title={$t('autoEncrypt.backup.title')} size="small" on:close={() => (backupFor = null)}>
    <form id="key-backup" class="dialog" on:submit|preventDefault={saveBackup}>
      <p>{$t('autoEncrypt.backup.text')}</p>
      <label for="key-backup-pass">{$t('autoEncrypt.backup.passphrase')}</label>
      <input id="key-backup-pass" class="cal-field" type="password" autocomplete="new-password" bind:value={backupPass} />
      <p class="meta" aria-live="polite">{backupPass !== '' && backupPass.length < 8 ? $t('autoEncrypt.backup.short') : ''}</p>
    </form>
    <svelte:fragment slot="footer">
      <button type="submit" form="key-backup" class="cal-btn primary" disabled={busy || backupPass.length < 8}>{$t('autoEncrypt.backup.save')}</button>
    </svelte:fragment>
  </Modal>
{/if}

{#if transferFor}
  <Modal title={$t('autoEncrypt.transfer.title')} size="small" closable={!busy} on:close={closeTransfer}>
    <div class="dialog">
      <p>{$t('autoEncrypt.transfer.text')}</p>
      {#if transferCode}
        <p class="meta">{$t('autoEncrypt.transfer.code')}</p>
        <code class="code" aria-label={$t('autoEncrypt.transfer.code')}>{transferCode}</code>
      {/if}
    </div>
    <svelte:fragment slot="footer">
      {#if transferCode}
        <button type="button" class="cal-btn primary" on:click={closeTransfer}>{$t('action.close')}</button>
      {:else}
        <button type="button" class="cal-btn primary" disabled={busy} on:click={sendTransfer}>{$t('autoEncrypt.transfer.send')}</button>
      {/if}
    </svelte:fragment>
  </Modal>
{/if}

<style>
  .where {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    margin-inline-end: var(--space-3);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .servers {
    width: 100%;
    margin-top: var(--space-2);
  }

  .own {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .own:last-child {
    border-bottom: none;
  }

  .own-head {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .email {
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
  }

  .fp,
  .code {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
    word-break: break-word;
  }

  .code {
    font-size: var(--fz-body);
    color: var(--text-primary);
    letter-spacing: 0.04em;
  }

  .meta {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .warn {
    color: var(--warning);
  }

  .own-actions {
    display: flex;
    gap: var(--space-2);
    flex-wrap: wrap;
  }

  .dialog {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .dialog p {
    margin: 0;
    line-height: 1.5;
  }
</style>
