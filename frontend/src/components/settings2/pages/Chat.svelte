<script lang="ts">
  // the Chat page: turning chat on, the account and its connection, this
  // device's encryption, the account's other sessions, and how chat notifies,
  // what it tells other people and how messages are composed.
  import { onMount } from 'svelte'
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import Segmented from '../ui/Segmented.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import Ph from '../../common/Ph.svelte'
  import ChatSignIn from '../../chat/ChatSignIn.svelte'
  import ChatUnlockDialog from '../../chat/ChatUnlockDialog.svelte'
  import ChatSetupDialog from '../../chat/ChatSetupDialog.svelte'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import {
    chatDevices,
    chatRefreshEncryption,
    chatSignOut,
    chatSignOutDevice,
    chatStartVerification,
  } from '../../../lib/api'
  import { chatError, chatSignedIn, chatStatus, refreshStatus } from '../../../stores/chat'
  import { askConfirm } from '../../../stores/confirm'
  import { toastError, toastInfo } from '../../../stores/toast'
  import {
    prefs,
    setChatEnabled,
    setChatMarkdown,
    setChatNotify,
    setChatSendReceipts,
    setChatSendTyping,
  } from '../../../stores/prefs'
  import { formatRelative } from '../../../lib/format'
  import { t } from '../../../lib/i18n'
  import type { ChatDevice, ChatNotifyMode } from '../../../lib/types'

  let unlockOpen = false
  let setupOpen = false
  let devices: ChatDevice[] = []
  let devicesError = ''
  let passwordFor = ''
  let password = ''
  let passwordError = ''
  let signingOut = false
  let loaded = false

  $: enabled = $prefs.chatEnabled
  $: s = $chatStatus
  $: enc = s.encryption
  $: signedIn = $chatSignedIn
  $: needsUnlock = enc.secretStorage && (!enc.deviceVerified || !enc.backupActive)

  $: if (enabled && signedIn && !loaded) {
    loaded = true
    void loadEncryption()
    void loadDevices()
  }
  $: if (!signedIn) {
    loaded = false
  }

  onMount(() => {
    if (enabled) {
      void refreshStatus()
    }
  })

  async function loadEncryption(): Promise<void> {
    try {
      await chatRefreshEncryption()
      await refreshStatus()
    } catch {
      // the state shown stays as the last event left it
    }
  }

  async function loadDevices(): Promise<void> {
    try {
      devices = await chatDevices()
      devicesError = ''
    } catch (err) {
      devicesError = chatError(err)
    }
  }

  async function signOut(): Promise<void> {
    const ok = await askConfirm({
      title: $t('chatSettings.signOut.title'),
      body: $t('chatSettings.signOut.body'),
      confirmLabel: $t('chatSettings.signOut'),
      danger: true,
    })
    if (!ok) {
      return
    }
    try {
      await chatSignOut()
      await refreshStatus()
      devices = []
    } catch (err) {
      toastError(chatError(err))
    }
  }

  async function verify(): Promise<void> {
    try {
      await chatStartVerification('')
      toastInfo($t('chatSettings.verify.sent'))
    } catch (err) {
      toastError(chatError(err))
    }
  }

  async function signOutDevice(id: string, withPassword: string): Promise<void> {
    signingOut = true
    passwordError = ''
    try {
      await chatSignOutDevice(id, withPassword)
      passwordFor = ''
      password = ''
      await loadDevices()
    } catch (err) {
      const text = chatError(err)
      if (!withPassword && /password/i.test(text)) {
        passwordFor = id
        password = ''
      } else if (passwordFor === id) {
        passwordError = text
      } else {
        toastError(text)
      }
    } finally {
      signingOut = false
    }
  }

  function cancelPassword(): void {
    passwordFor = ''
    password = ''
    passwordError = ''
  }

  $: stateLabel =
    s.state === 'online'
      ? $t('chatSettings.state.online')
      : s.state === 'starting' || s.state === 'syncing'
        ? $t('chatSettings.state.syncing')
        : $t('chatSettings.state.offline')

  $: notifyOptions = [
    { key: 'all', label: $t('chatSettings.notify.all') },
    { key: 'mentions', label: $t('chatSettings.notify.mentions') },
    { key: 'off', label: $t('chatSettings.notify.off') },
  ]

  $: initial = (s.displayName || s.userId.replace(/^@/, '')).charAt(0).toUpperCase()
</script>

<Card title={$t('chatSettings.title')}>
  <Row id="chat-enabled" label={$t('chatSettings.enabled')} hint={$t('chatSettings.enabled.hint')}>
    <ToggleSwitch checked={enabled} label={$t('chatSettings.enabled')} on:change={(e) => setChatEnabled(e.detail)} />
  </Row>
</Card>

{#if enabled}
  <Card title={$t('chatSettings.account')}>
    {#if !signedIn}
      <div class="block" data-setting="chat-account">
        <ChatSignIn on:signedin={() => void refreshStatus()} />
      </div>
    {:else}
      <div class="block who" data-setting="chat-account">
        <span class="avatar" aria-hidden="true">{initial}</span>
        <span class="who-text">
          <span class="who-name">{s.displayName || s.userId}</span>
          <span class="mono">{s.userId}</span>
        </span>
        <button type="button" class="action" on:click={signOut}>{$t('chatSettings.signOut')}</button>
      </div>
      <Row id="chat-homeserver" label={$t('chatSettings.homeserver')}>
        <span class="mono value">{s.homeserver}</span>
      </Row>
      <Row id="chat-session" label={$t('chatSettings.session')} hint={$t('chatSettings.session.hint')}>
        <span class="mono value">{s.deviceId}</span>
      </Row>
      <Row id="chat-connection" label={$t('chatSettings.connection')} hint={s.error}>
        <span class="tag" class:ok={s.state === 'online'} class:warn={s.state !== 'online' && s.state !== 'starting' && s.state !== 'syncing'}>
          {stateLabel}
        </span>
      </Row>
    {/if}
  </Card>

  {#if signedIn}
    <Card title={$t('chatSettings.encryption')} description={$t('chatSettings.encryption.hint')}>
      {#if !enc.deviceVerified}
        <div class="block alert" role="alert" data-setting="chat-device-verified">
          <span class="alert-icon"><Ph svg={warningIcon} size={18} /></span>
          <span class="alert-text">
            <span class="alert-title">{$t('chatSettings.unverified.title')}</span>
            <span class="alert-body">{$t('chatSettings.unverified.body')}</span>
          </span>
        </div>
      {:else}
        <Row id="chat-device-verified" label={$t('chatSettings.deviceVerified')}>
          <span class="tag ok">{$t('chatSettings.verified')}</span>
        </Row>
      {/if}
      <Row id="chat-cross-signing" label={$t('chatSettings.crossSigning')} hint={$t('chatSettings.crossSigning.hint')}>
        <span class="tag" class:ok={enc.crossSigning}>{enc.crossSigning ? $t('chatSettings.on') : $t('chatSettings.off')}</span>
      </Row>
      <Row id="chat-secure-backup" label={$t('chatSettings.secureBackup')} hint={$t('chatSettings.secureBackup.hint')}>
        <span class="tag" class:ok={enc.secretStorage}>{enc.secretStorage ? $t('chatSettings.on') : $t('chatSettings.off')}</span>
      </Row>
      <Row
        id="chat-key-backup"
        label={$t('chatSettings.keyBackup')}
        hint={enc.backupActive ? $t('chatSettings.keyBackup.count').replace('{n}', String(enc.backupKeys)).replace('{version}', enc.backupVersion) : $t('chatSettings.keyBackup.hint')}
      >
        <span class="tag" class:ok={enc.backupActive}>{enc.backupActive ? $t('chatSettings.active') : $t('chatSettings.inactive')}</span>
      </Row>
      {#if enc.fingerprint}
        <Row id="chat-fingerprint" label={$t('chatSettings.fingerprint')} hint={$t('chatSettings.fingerprint.hint')}>
          <span class="mono value">{enc.fingerprint}</span>
        </Row>
      {/if}
      <Row id="chat-encryption-actions" label={$t('chatSettings.actions')}>
        <button type="button" class="action" on:click={verify}>{$t('chatSettings.verify')}</button>
        {#if needsUnlock}
          <button type="button" class="action" on:click={() => (unlockOpen = true)}>{$t('chatSettings.unlock')}</button>
        {/if}
        {#if !enc.secretStorage}
          <button type="button" class="action" on:click={() => (setupOpen = true)}>{$t('chatSettings.setup')}</button>
        {/if}
      </Row>
    </Card>

    <Card title={$t('chatSettings.sessions')} description={$t('chatSettings.sessions.hint')}>
      {#if devicesError}
        <p class="block empty" data-setting="chat-sessions">{devicesError}</p>
      {:else if devices.length === 0}
        <p class="block empty" data-setting="chat-sessions">{$t('chatSettings.sessions.none')}</p>
      {/if}
      {#each devices as d (d.id)}
        <div class="block device" data-setting={d.current ? 'chat-sessions' : undefined}>
          <div class="device-line">
            <span class="device-text">
              <span class="device-name">{d.name || d.id}</span>
              <span class="device-meta">
                <span class="mono">{d.id}</span>
                {#if d.lastSeen > 0}
                  <span>{formatRelative(d.lastSeen, $t)}</span>
                {/if}
              </span>
            </span>
            <span class="device-tags">
              {#if d.current}
                <span class="tag">{$t('chatSettings.thisSession')}</span>
              {/if}
              <span class="tag" class:ok={d.verified}>{d.verified ? $t('chatSettings.verified') : $t('chatSettings.notVerified')}</span>
              {#if !d.current}
                <button type="button" class="action" disabled={signingOut} on:click={() => signOutDevice(d.id, '')}>
                  {$t('chatSettings.signOutSession')}
                </button>
              {/if}
            </span>
          </div>
          {#if passwordFor === d.id}
            <form class="password" on:submit|preventDefault={() => signOutDevice(d.id, password)}>
              <label for="chat-device-password">{$t('chatSettings.password.label')}</label>
              <div class="password-line">
                <input id="chat-device-password" type="password" bind:value={password} autocomplete="current-password" />
                <button type="submit" class="primary" disabled={signingOut || !password}>{$t('chatSettings.signOutSession')}</button>
                <button type="button" class="action" on:click={cancelPassword}>{$t('chatSettings.cancel')}</button>
              </div>
              {#if passwordError}
                <p class="error" role="alert">{passwordError}</p>
              {/if}
            </form>
          {/if}
        </div>
      {/each}
    </Card>
  {/if}

  <Card title={$t('chatSettings.notifications')}>
    <Row id="chat-notify" label={$t('chatSettings.notify')} hint={$t('chatSettings.notify.hint')}>
      <Segmented value={$prefs.chatNotify} options={notifyOptions} label={$t('chatSettings.notify')} on:change={(e) => setChatNotify(e.detail as ChatNotifyMode)} />
    </Row>
  </Card>

  <Card title={$t('chatSettings.privacy')}>
    <Row id="chat-receipts" label={$t('chatSettings.receipts')} hint={$t('chatSettings.receipts.hint')}>
      <ToggleSwitch checked={$prefs.chatSendReceipts} label={$t('chatSettings.receipts')} on:change={(e) => setChatSendReceipts(e.detail)} />
    </Row>
    <Row id="chat-typing" label={$t('chatSettings.typing')} hint={$t('chatSettings.typing.hint')}>
      <ToggleSwitch checked={$prefs.chatSendTyping} label={$t('chatSettings.typing')} on:change={(e) => setChatSendTyping(e.detail)} />
    </Row>
  </Card>

  <Card title={$t('chatSettings.composing')}>
    <Row id="chat-markdown" label={$t('chatSettings.markdown')} hint={$t('chatSettings.markdown.hint')}>
      <ToggleSwitch checked={$prefs.chatMarkdown} label={$t('chatSettings.markdown')} on:change={(e) => setChatMarkdown(e.detail)} />
    </Row>
  </Card>
{/if}

{#if unlockOpen}
  <ChatUnlockDialog passphraseAvailable={enc.secretStoragePassphrase} on:close={() => (unlockOpen = false)} />
{/if}
{#if setupOpen}
  <ChatSetupDialog on:close={() => (setupOpen = false)} />
{/if}

<style>
  .block {
    padding: var(--space-3) var(--space-4);
  }

  .mono {
    font-family: var(--font-mono);
    font-size: var(--fz-label);
    color: var(--text-secondary);
    overflow-wrap: anywhere;
  }

  .value {
    text-align: end;
    max-width: 40ch;
  }

  .who {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }

  .avatar {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: calc(var(--control-height) + var(--space-2));
    height: calc(var(--control-height) + var(--space-2));
    border-radius: 50%;
    background: var(--surface-sunken);
    border: var(--hairline) solid var(--border-default);
    font-size: var(--fz-list);
    font-weight: var(--fw-semibold);
    color: var(--text-secondary);
    flex-shrink: 0;
  }

  .who-text {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
  }

  .who-name {
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .tag {
    display: inline-flex;
    align-items: center;
    padding: 0 var(--space-2);
    height: calc(var(--control-height) - var(--space-3));
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
    white-space: nowrap;
  }

  .tag.ok {
    border-color: transparent;
    background: var(--success-bg);
    color: var(--success);
  }

  .tag.warn {
    border-color: transparent;
    background: var(--warning-bg);
    color: var(--warning);
  }

  .alert {
    display: flex;
    gap: var(--space-3);
    border-inline-start: 2px solid var(--warning);
    background: var(--warning-bg);
  }

  .alert-icon {
    display: inline-flex;
    padding-top: 1px;
    color: var(--warning);
  }

  .alert-text {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .alert-title {
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .alert-body {
    font-size: var(--fz-label);
    line-height: 1.4;
    color: var(--text-secondary);
  }

  .empty {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .device {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .device-line {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
  }

  .device-text {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .device-name {
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .device-meta {
    display: flex;
    gap: var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .device-tags {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex-shrink: 0;
  }

  .password {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .password label {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .password-line {
    display: flex;
    gap: var(--space-2);
  }

  input {
    width: 100%;
    min-width: 0;
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-primary);
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    outline: none;
    box-sizing: border-box;
  }

  input:focus {
    border-color: var(--accent);
  }

  .error {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--danger);
  }

  .action,
  .primary {
    padding: var(--space-2) var(--space-4);
    border-radius: var(--radius-control);
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    white-space: nowrap;
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .action {
    border: var(--hairline) solid var(--border-default);
    background: var(--surface-raised);
    color: var(--text-primary);
  }

  .action:hover:not(:disabled) {
    background: var(--surface-hover);
  }

  .primary {
    border: none;
    background: var(--accent);
    color: var(--accent-fg);
    font-weight: var(--fw-semibold);
  }

  .action:disabled,
  .primary:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
