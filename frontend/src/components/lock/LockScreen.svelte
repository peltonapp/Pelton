<script lang="ts">
  // the app lock screen: the Pelton mark, one line saying the app is locked,
  // and the way back in. With the system prompt it is an Unlock button, pressed
  // once by itself when the window first has focus; with a passphrase it is a
  // field, and wrong attempts past the first few wait longer each time. It
  // fills the window on its own: nothing of the mail is behind it.
  import { createEventDispatcher, onDestroy, onMount, tick } from 'svelte'
  import unlockIcon from '@phosphor-icons/core/assets/duotone/lock-key-open-duotone.svg?raw'
  import mark from '../../assets/images/icons/pelton-logo.png'
  import Ph from '../common/Ph.svelte'
  import WindowButtons from '../shell/WindowButtons.svelte'
  import { quitApp, unlockWithPassphrase, unlockWithSystem } from '../../lib/api'
  import { t } from '../../lib/i18n'
  import type { LockAuthResult, LockState } from '../../lib/types'

  /** The lock state as the backend last reported it. */
  export let state: LockState
  /** Draws the window buttons, for a window whose title bar is the interface. */
  export let windowButtons = false

  const dispatch = createEventDispatcher<{ unlocked: void }>()

  $: system = state.method === 'system'

  let passphrase = ''
  let busy = false
  let message = ''
  // seconds until the next passphrase attempt is allowed.
  let waitLeft = 0
  let waitTimer: ReturnType<typeof setInterval> | undefined
  let field: HTMLInputElement | null = null
  let autoTried = false

  function startWait(ms: number): void {
    clearInterval(waitTimer)
    const until = Date.now() + ms
    const update = (): void => {
      waitLeft = Math.max(0, Math.ceil((until - Date.now()) / 1000))
      if (waitLeft === 0) {
        clearInterval(waitTimer)
        void tick().then(() => field?.focus())
      }
    }
    update()
    waitTimer = setInterval(update, 250)
  }

  function explain(res: LockAuthResult): void {
    if (res.retryAfterMs && res.retryAfterMs > 0) {
      startWait(res.retryAfterMs)
    }
    switch (res.reason) {
      case 'wrong':
        message = $t('applock.wrong')
        break
      case 'wait':
        message = ''
        break
      case 'cancelled':
        message = $t('applock.cancelled')
        break
      case 'busy':
        message = $t('applock.busy')
        break
      default:
        message = $t('applock.unavailable')
    }
  }

  async function unlockSystem(): Promise<void> {
    if (busy) {
      return
    }
    busy = true
    message = ''
    try {
      const res = await unlockWithSystem($t('applock.reason.unlock'))
      if (res.ok) {
        dispatch('unlocked')
      } else {
        explain(res)
      }
    } catch {
      message = $t('applock.unavailable')
    } finally {
      busy = false
    }
  }

  async function submit(): Promise<void> {
    if (busy || waitLeft > 0 || passphrase === '') {
      return
    }
    busy = true
    message = ''
    try {
      const res = await unlockWithPassphrase(passphrase)
      passphrase = ''
      if (res.ok) {
        dispatch('unlocked')
        return
      }
      explain(res)
    } catch {
      message = $t('applock.unavailable')
    } finally {
      busy = false
    }
    await tick()
    field?.focus()
  }

  // the system prompt comes up by itself once, the first time the window has
  // focus, so unlocking is a touch away without a click first.
  function autoUnlock(): void {
    if (autoTried || !system) {
      return
    }
    autoTried = true
    void unlockSystem()
  }

  onMount(() => {
    if (state.retryAfterMs > 0) {
      startWait(state.retryAfterMs)
    }
    if (system) {
      if (document.hasFocus()) {
        autoUnlock()
      }
    } else {
      field?.focus()
    }
  })

  onDestroy(() => clearInterval(waitTimer))
</script>

<svelte:window on:focus={autoUnlock} />

<div class="lock" role="dialog" aria-modal="true" aria-labelledby="lock-title">
  <div class="bar" style="--wails-draggable:drag">
    {#if windowButtons}
      <WindowButtons />
    {/if}
  </div>

  <div class="panel">
    <img class="mark" src={mark} alt="" draggable="false" />
    <h1 id="lock-title">{$t('applock.title')}</h1>
    <p class="lead">{system ? $t('applock.leadSystem') : $t('applock.leadPassphrase')}</p>

    {#if system}
      <button type="button" class="primary" disabled={busy} on:click={unlockSystem}>
        <Ph svg={unlockIcon} size={16} />
        <span>{$t('applock.unlock')}</span>
      </button>
    {:else if state.method === 'passphrase'}
      <form on:submit|preventDefault={submit}>
        <input
          bind:this={field}
          bind:value={passphrase}
          type="password"
          autocomplete="off"
          spellcheck="false"
          aria-label={$t('applock.passphrase')}
          placeholder={$t('applock.passphrase')}
          disabled={busy || waitLeft > 0}
        />
        <button type="submit" class="primary" disabled={busy || waitLeft > 0 || passphrase === ''}>
          <Ph svg={unlockIcon} size={16} />
          <span>{$t('applock.unlock')}</span>
        </button>
      </form>
    {/if}

    <p class="message" role="alert">
      {#if waitLeft > 0}
        {$t('applock.wait').replace('{seconds}', String(waitLeft))}
      {:else}
        {message}
      {/if}
    </p>
  </div>

  <button type="button" class="quit" on:click={quitApp}>{$t('applock.quit')}</button>
</div>

<style>
  .lock {
    position: fixed;
    inset: 0;
    z-index: 2000;
    display: flex;
    flex-direction: column;
    align-items: center;
    background: var(--surface-base);
    color: var(--text-primary);
    font-family: var(--font-ui);
  }

  .bar {
    align-self: stretch;
    display: flex;
    justify-content: flex-end;
    min-height: max(var(--topbar-height), var(--titlebar-lights));
    user-select: none;
  }

  .panel {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    width: var(--lock-width);
    max-width: calc(100% - 2 * var(--space-6));
    padding-bottom: var(--topbar-height);
    text-align: center;
  }

  .mark {
    width: var(--lock-mark);
    height: var(--lock-mark);
    object-fit: contain;
    margin-bottom: var(--space-3);
    user-select: none;
    -webkit-user-select: none;
  }

  h1 {
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
  }

  .lead {
    margin: 0 0 var(--space-3);
    font-size: var(--fz-label);
    line-height: 1.4;
    color: var(--text-secondary);
  }

  form {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    width: 100%;
  }

  input {
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-list);
    transition: border-color var(--motion-fast) var(--ease-out);
  }

  input:focus {
    outline: none;
    border-color: var(--accent);
  }

  input::placeholder {
    color: var(--text-tertiary);
  }

  .primary {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
    height: var(--control-height);
    padding: 0 var(--space-5);
    border: none;
    border-radius: var(--radius-control);
    background: var(--accent);
    color: var(--accent-fg);
    font: inherit;
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  form .primary {
    width: 100%;
  }

  .primary:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent) 88%, var(--text-primary));
  }

  .primary:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .message {
    min-height: calc(var(--fz-label) * 1.4);
    margin: 0;
    font-size: var(--fz-label);
    line-height: 1.4;
    color: var(--danger);
  }

  .quit {
    margin-bottom: var(--space-5);
    padding: var(--space-2) var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .quit:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
</style>
