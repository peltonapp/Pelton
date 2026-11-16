<script lang="ts">
  // the app lock settings: turning the lock on and off, when it locks by
  // itself, and what notifications show while locked. Turning it off or
  // changing the passphrase proves the user first, with the system prompt or
  // the current passphrase; the backend checks, not this page.
  import { onMount } from 'svelte'
  import Card from './ui/Card.svelte'
  import Row from './ui/Row.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import Select from '../common/Select.svelte'
  import { prefs, noteAppLock, setAppLockIdleMinutes, setAppLockOnLaunch, setAppLockHideNotifications } from '../../stores/prefs'
  import { changeLockPassphrase, disableAppLock, enableAppLock, getLockState, lockNow } from '../../lib/api'
  import { errorMessage, toastInfo } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { LockAuthResult, LockState } from '../../lib/types'
  import '../../theme/settings2-sieve.css'

  /** The shortest passphrase the backend accepts. */
  const minPassphrase = 6

  let state: LockState | null = null
  // the passphrase form on show: setting one to turn the lock on, the current
  // one to turn it off, or both to change it.
  let form: 'enable' | 'disable' | 'change' | null = null
  let current = ''
  let next = ''
  let repeat = ''
  let busy = false
  let error = ''

  $: on = $prefs.appLock
  $: passphraseLock = on && state?.method === 'passphrase'

  $: idleOptions = [
    { value: '0', label: $t('applock.settings.idleNever') },
    { value: '1', label: $t('applock.settings.minute') },
    ...[5, 10, 15, 30, 60].map((m) => ({ value: String(m), label: $t('applock.settings.minutes').replace('{count}', String(m)) })),
  ]

  async function refresh(): Promise<void> {
    try {
      state = await getLockState()
      noteAppLock(state.enabled)
    } catch {
      state = null
    }
  }

  onMount(refresh)

  function reasonText(res: LockAuthResult): string {
    switch (res.reason) {
      case 'wrong':
        return $t('applock.wrong')
      case 'wait':
        return $t('applock.wait').replace('{seconds}', String(Math.ceil((res.retryAfterMs ?? 0) / 1000)))
      case 'cancelled':
        return $t('applock.cancelled')
      case 'busy':
        return $t('applock.busy')
      case 'short':
        return $t('applock.settings.short').replace('{count}', String(minPassphrase))
      default:
        return $t('applock.unavailable')
    }
  }

  function openForm(which: 'enable' | 'disable' | 'change'): void {
    form = which
    current = ''
    next = ''
    repeat = ''
    error = ''
  }

  function closeForm(): void {
    form = null
    current = ''
    next = ''
    repeat = ''
  }

  async function run(op: () => Promise<LockAuthResult>, done?: string): Promise<void> {
    busy = true
    error = ''
    try {
      const res = await op()
      if (res.ok) {
        closeForm()
        if (done) {
          toastInfo(done)
        }
      } else {
        error = reasonText(res)
      }
    } catch (err) {
      error = errorMessage(err)
    } finally {
      busy = false
      current = ''
      await refresh()
    }
  }

  function onToggle(wanted: boolean): void {
    error = ''
    if (wanted) {
      if (state?.systemAvailable) {
        void run(() => enableAppLock('', $t('applock.reason.enable')))
      } else {
        openForm('enable')
      }
      return
    }
    if (state?.method === 'system') {
      void run(() => disableAppLock('', $t('applock.reason.disable')))
    } else {
      openForm('disable')
    }
  }

  // newPassphraseError checks the new passphrase and its repeat before they
  // are sent, so a typo is caught here.
  function newPassphraseError(): string {
    if ([...next].length < minPassphrase) {
      return $t('applock.settings.short').replace('{count}', String(minPassphrase))
    }
    if (next !== repeat) {
      return $t('applock.settings.mismatch')
    }
    return ''
  }

  function submit(): void {
    if (busy) {
      return
    }
    if (form === 'disable') {
      void run(() => disableAppLock(current, ''))
      return
    }
    const problem = newPassphraseError()
    if (problem) {
      error = problem
      return
    }
    if (form === 'enable') {
      void run(() => enableAppLock(next, ''))
    } else if (form === 'change') {
      void run(() => changeLockPassphrase(current, next), $t('applock.settings.changed'))
    }
  }
</script>

<Card title={$t('applock.settings.title')}>
  <Row id="app-lock" label={$t('applock.settings.toggle')} hint={$t('applock.settings.hint')}>
    <ToggleSwitch checked={on} disabled={busy || state === null} label={$t('applock.settings.toggle')} on:change={(e) => onToggle(e.detail)} />
  </Row>
  {#if !on && state && !state.systemAvailable && form !== 'enable'}
    <p class="note">{$t('applock.settings.passphraseNote')}</p>
  {/if}
  {#if on && state}
    <p class="note">{state.method === 'system' ? $t('applock.settings.methodSystem') : $t('applock.settings.methodPassphrase')}</p>
  {/if}

  {#if form}
    <form class="lock-form" on:submit|preventDefault={submit}>
      {#if form !== 'enable'}
        <input class="sv-input" type="password" autocomplete="off" bind:value={current} placeholder={$t('applock.settings.current')} aria-label={$t('applock.settings.current')} />
      {/if}
      {#if form !== 'disable'}
        <input class="sv-input" type="password" autocomplete="off" bind:value={next} placeholder={$t('applock.settings.new')} aria-label={$t('applock.settings.new')} />
        <input class="sv-input" type="password" autocomplete="off" bind:value={repeat} placeholder={$t('applock.settings.repeat')} aria-label={$t('applock.settings.repeat')} />
      {/if}
      <div class="actions">
        <button type="button" class="s2-btn quiet" on:click={closeForm}>{$t('settingsPanel.button.cancel')}</button>
        <button type="submit" class="s2-btn primary" disabled={busy}>
          {form === 'enable' ? $t('applock.settings.turnOn') : form === 'disable' ? $t('applock.settings.turnOff') : $t('applock.settings.save')}
        </button>
      </div>
    </form>
  {/if}
  {#if error}
    <p class="error" role="alert">{error}</p>
  {/if}

  {#if on}
    <Row id="app-lock-idle" label={$t('applock.settings.idle')} hint={$t('applock.settings.idleHint')}>
      <Select
        value={String($prefs.appLockIdleMinutes)}
        ariaLabel={$t('applock.settings.idle')}
        items={idleOptions}
        on:change={(e) => setAppLockIdleMinutes(Number(e.detail))}
      />
    </Row>
    <Row id="app-lock-launch" label={$t('applock.settings.onLaunch')}>
      <ToggleSwitch checked={$prefs.appLockOnLaunch} label={$t('applock.settings.onLaunch')} on:change={(e) => setAppLockOnLaunch(e.detail)} />
    </Row>
    <Row id="app-lock-notifications" label={$t('applock.settings.hideNotifications')} hint={$t('applock.settings.hideNotificationsHint')}>
      <ToggleSwitch
        checked={$prefs.appLockHideNotifications}
        label={$t('applock.settings.hideNotifications')}
        on:change={(e) => setAppLockHideNotifications(e.detail)}
      />
    </Row>
    <Row id="app-lock-now" label={$t('applock.settings.lockNow')} hint={$t('applock.settings.lockNowHint')}>
      <div class="buttons">
        {#if passphraseLock && form !== 'change'}
          <button type="button" class="s2-btn" on:click={() => openForm('change')}>{$t('applock.settings.change')}</button>
        {/if}
        <button type="button" class="s2-btn" on:click={() => void lockNow()}>{$t('applock.settings.lockNow')}</button>
      </div>
    </Row>
  {/if}
</Card>

<style>
  .note,
  .error {
    margin: 0;
    padding: var(--space-3) var(--space-4);
    font-size: var(--fz-label);
    line-height: 1.4;
    color: var(--text-tertiary);
  }

  .error {
    color: var(--danger);
  }

  .lock-form {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .actions,
  .buttons {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-3);
  }
</style>
