<script lang="ts">
  // the chat sign-in form: a server or Matrix ID is looked up first, then the
  // sign-in methods that server offers are shown. it has no surface of its own,
  // so a page or a space can place it where it needs it, and it reports a
  // finished sign-in with a `signedin` event.
  import { createEventDispatcher } from 'svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import { chatCancelSSO, chatDiscover, chatLoginPassword, chatLoginSSO, chatLoginToken } from '../../lib/api'
  import { chatError, refreshStatus } from '../../stores/chat'
  import { t } from '../../lib/i18n'
  import type { ChatServer } from '../../lib/types'

  const dispatch = createEventDispatcher<{ signedin: void }>()

  let input = ''
  let server: ChatServer | null = null
  let user = ''
  let password = ''
  let token = ''
  let tokenOpen = false
  let finding = false
  let busy = false
  let waitingSSO = false
  let error = ''

  $: methods = server ? Number(server.password) + Number(server.sso) + Number(server.token) : 0

  async function discover(): Promise<void> {
    const value = input.trim()
    if (!value || finding) {
      return
    }
    finding = true
    error = ''
    try {
      server = await chatDiscover(value)
      user = server.user
    } catch (err) {
      server = null
      error = chatError(err)
    } finally {
      finding = false
    }
  }

  function change(): void {
    server = null
    error = ''
    password = ''
    token = ''
    tokenOpen = false
  }

  async function finish(run: () => Promise<void>): Promise<void> {
    busy = true
    error = ''
    try {
      await run()
      await refreshStatus()
      dispatch('signedin')
    } catch (err) {
      error = chatError(err)
    } finally {
      busy = false
      waitingSSO = false
    }
  }

  function signInPassword(): void {
    if (!server || !user.trim() || !password) {
      return
    }
    const hs = server.homeserver
    void finish(() => chatLoginPassword(hs, user.trim(), password))
  }

  function signInSSO(providerId: string): void {
    if (!server) {
      return
    }
    const hs = server.homeserver
    waitingSSO = true
    void finish(() => chatLoginSSO(hs, providerId))
  }

  function signInToken(): void {
    if (!server || !token.trim()) {
      return
    }
    const hs = server.homeserver
    void finish(() => chatLoginToken(hs, token.trim()))
  }

  function cancelSSO(): void {
    void chatCancelSSO().catch(() => {})
  }
</script>

<div class="signin">
  {#if !server}
    <form class="find" on:submit|preventDefault={discover}>
      <label for="chat-server">{$t('chat.signin.server')}</label>
      <div class="line">
        <input
          id="chat-server"
          bind:value={input}
          placeholder={$t('chat.signin.serverPlaceholder')}
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
          disabled={finding}
        />
        <button type="submit" class="primary" disabled={finding || input.trim() === ''}>
          {#if finding}
            <PieProgress size={14} label={$t('chat.signin.finding')} />
          {/if}
          {$t('chat.signin.continue')}
        </button>
      </div>
    </form>
  {:else}
    <div class="found">
      <span class="found-text">
        <span class="found-label">{$t('chat.signin.homeserver')}</span>
        <span class="found-name">{server.homeserver}</span>
      </span>
      <button type="button" class="link" disabled={busy} on:click={change}>{$t('chat.signin.change')}</button>
    </div>

    {#if methods === 0}
      <p class="note">{$t('chat.signin.noMethods')}</p>
    {/if}

    {#if server.password}
      <form class="method" on:submit|preventDefault={signInPassword}>
        <label for="chat-user">{$t('chat.signin.user')}</label>
        <input id="chat-user" bind:value={user} autocomplete="username" autocapitalize="off" spellcheck="false" disabled={busy} />
        <label for="chat-password">{$t('chat.signin.password')}</label>
        <input id="chat-password" type="password" bind:value={password} autocomplete="current-password" disabled={busy} />
        <div class="actions">
          <button type="submit" class="primary" disabled={busy || !user.trim() || !password}>
            {#if busy && !waitingSSO}
              <PieProgress size={14} label={$t('chat.signin.signingIn')} />
            {/if}
            {$t('chat.signin.signIn')}
          </button>
        </div>
      </form>
    {/if}

    {#if server.sso}
      <div class="method">
        {#if waitingSSO}
          <div class="waiting" role="status">
            <PieProgress size={14} label={$t('chat.signin.waiting')} />
            <span>{$t('chat.signin.waitingHint')}</span>
            <button type="button" class="action" on:click={cancelSSO}>{$t('chat.signin.cancel')}</button>
          </div>
        {:else if server.providers.length > 0}
          {#each server.providers as provider (provider.id)}
            <button type="button" class="action wide" disabled={busy} on:click={() => signInSSO(provider.id)}>
              {$t('chat.signin.continueWith').replace('{name}', provider.name)}
            </button>
          {/each}
        {:else}
          <button type="button" class="action wide" disabled={busy} on:click={() => signInSSO('')}>
            {$t('chat.signin.sso')}
          </button>
        {/if}
      </div>
    {/if}

    {#if server.token}
      <div class="method">
        <button type="button" class="link start" aria-expanded={tokenOpen} on:click={() => (tokenOpen = !tokenOpen)}>
          {$t('chat.signin.token')}
        </button>
        {#if tokenOpen}
          <form class="token" on:submit|preventDefault={signInToken}>
            <label for="chat-token">{$t('chat.signin.tokenLabel')}</label>
            <div class="line">
              <input id="chat-token" bind:value={token} autocomplete="off" spellcheck="false" disabled={busy} />
              <button type="submit" class="action" disabled={busy || !token.trim()}>{$t('chat.signin.signIn')}</button>
            </div>
          </form>
        {/if}
      </div>
    {/if}
  {/if}

  {#if error}
    <p class="error" role="alert">{error}</p>
  {/if}
</div>

<style>
  .signin {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    min-width: 0;
  }

  form,
  .method {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  label {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .line {
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

  .found {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }

  .found-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .found-label {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .found-name {
    font-family: var(--font-mono);
    font-size: var(--fz-label);
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  .note {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    margin-top: var(--space-1);
  }

  .waiting {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .waiting span {
    flex: 1;
  }

  .token {
    margin-top: var(--space-1);
  }

  .error {
    margin: 0;
    padding: var(--space-2) var(--space-3);
    border-inline-start: 2px solid var(--danger);
    background: var(--danger-bg);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .primary,
  .action {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
    height: var(--control-height);
    padding: 0 var(--space-4);
    border-radius: var(--radius-control);
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    white-space: nowrap;
  }

  .primary {
    border: none;
    background: var(--accent);
    color: var(--accent-fg);
    font-weight: var(--fw-semibold);
  }

  .action {
    border: var(--hairline) solid var(--border-default);
    background: var(--surface-raised);
    color: var(--text-primary);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .action:hover:not(:disabled) {
    background: var(--surface-hover);
  }

  .wide {
    width: 100%;
  }

  .primary:disabled,
  .action:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .link {
    padding: 0;
    border: none;
    background: transparent;
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    color: var(--link);
    cursor: var(--cursor-action);
  }

  .link.start {
    align-self: flex-start;
  }

  .link:hover:not(:disabled) {
    text-decoration: underline;
  }

  .link:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
