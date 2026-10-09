<script lang="ts">
  // asks for a mailbox password at sync time.
  //
  // An account imported from another mail client has no password: the import
  // brings the server settings across but the other client's stored secret is
  // not readable. Without this the account simply never syncs and never says
  // why, so the sync path raises this prompt instead.
  //
  // Cancelling is a real answer. It skips that account for this sync rather
  // than blocking the others, and the same prompt comes back next time.
  // "Don't ask again" is the stronger form: the prompt stops interrupting for
  // that account entirely and a warning marker next to the mailbox takes over
  // saying it cannot sync (#290).
  import { IconLock } from '@tabler/icons-svelte'
  import Modal from '../common/Modal.svelte'
  import { checkAccountPassword, setAccountPassword } from '../../lib/api'
  import { errorMessage, toastError, toastInfo } from '../../stores/toast'
  import { retrySync } from '../../stores/syncfailures'
  import { t } from '../../lib/i18n'
  import type { PasswordPromptResult } from '../../stores/passwordprompt'
  import { accountLabel } from '../../lib/format'
  import type { Account } from '../../lib/types'

  /** The account being asked about, or null when the prompt is closed. */
  export let account: Account | null = null
  /** Called with how the prompt ended. */
  export let onDone: (result: PasswordPromptResult) => void = () => {}

  let password = ''
  let busy = false
  let seededFor: unknown = null
  // set once the server has refused what is typed. It turns the error line on
  // and offers to store it anyway, for the case where the server is wrong and
  // you know better.
  let refused = false

  // the address, not the display name. Several mailboxes named "Work" and
  // "Private" all look the same in this prompt, and the address is the part
  // that says which login is being asked for, so it is the part that must
  // never be the one dropped. The name comes along when there is one and it
  // adds something.
  $: who = account ? namedAccount(account) : ''

  function namedAccount(account: Account): string {
    const label = accountLabel(account)
    return label === account.email ? account.email : `${label} (${account.email})`
  }

  $: if (account !== seededFor) {
    seededFor = account
    password = ''
    busy = false
    refused = false
  }

  // typing again is the answer to a refusal, so the error clears with the first
  // keystroke rather than sitting under a password it no longer describes.
  function onInput(): void {
    refused = false
  }

  // submit tries the password against the server before storing it. Waiting for
  // the next sync to find out it is still wrong is the whole bug: a mailbox that
  // was marked stays marked and nothing says why.
  async function submit(): Promise<void> {
    if (!account || busy || password === '') {
      return
    }
    busy = true
    try {
      const result = await checkAccountPassword(account.id, password)
      if (result.rejected) {
        refused = true
        return
      }
      // anything else that went wrong is the server being unreachable, which
      // says nothing about the password. Store it and say it is unchecked; the
      // marker clears on the first sync that gets through.
      await store()
      if (!result.ok) {
        toastInfo($t('mailboxes.passwordPrompt.unverified'))
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  // saveAnyway skips the check. The server refused it, but a server can be
  // wrong about its own users, so this is not a dead end.
  async function saveAnyway(): Promise<void> {
    if (!account || busy || password === '') {
      return
    }
    busy = true
    try {
      await store()
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  async function store(): Promise<void> {
    if (!account) {
      return
    }
    await setAccountPassword(account.id, password)
    // the mailbox could not sync without it, so it syncs now rather than on
    // the next run.
    void retrySync(account.id)
    password = ''
    refused = false
    onDone('saved')
  }

  function cancel(): void {
    password = ''
    refused = false
    onDone('skipped')
  }

  function dismiss(): void {
    password = ''
    refused = false
    onDone('dismissed')
  }

</script>

{#if account}
  <Modal title={$t('mailboxes.passwordPrompt.title')} size="small" on:close={cancel}>
    <span slot="icon"><IconLock size={16} stroke={1.8} /></span>

    <p class="lead">
      {$t('mailboxes.passwordPrompt.body').replace('{email}', who)}
    </p>
    <p class="lead">{$t('mailboxes.passwordPrompt.dismissHint')}</p>

    <form on:submit|preventDefault={submit}>
      <label class="field">
        <span>{$t('wizard.field.password')}</span>
        <!-- svelte-ignore a11y-autofocus -->
        <input type="password" bind:value={password} on:input={onInput} autofocus autocomplete="off" spellcheck="false" />
      </label>

      {#if refused}
        <p class="refused" role="alert">{$t('mailboxes.passwordPrompt.refused')}</p>
      {/if}
    </form>

    <svelte:fragment slot="footer">
      <button type="button" class="dismiss" on:click={dismiss}>
        {$t('mailboxes.passwordPrompt.dismiss')}
      </button>
      <button type="button" class="cancel" on:click={cancel}>
        {$t('mailboxes.passwordPrompt.skip')}
      </button>
      {#if refused}
        <button type="button" class="cancel" disabled={busy} on:click={saveAnyway}>
          {$t('mailboxes.passwordPrompt.saveAnyway')}
        </button>
      {/if}
      <button type="button" class="go" disabled={busy || password === ''} on:click={submit}>
        {busy ? $t('mailboxes.passwordPrompt.checking') : $t('mailboxes.save')}
      </button>
    </svelte:fragment>
  </Modal>
{/if}

<style>

  .lead {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  form {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .field span {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .refused {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--danger);
  }

  .field input {
    padding: var(--space-2) var(--space-3);
    font-family: var(--font-ui);
    font-size: var(--fz-body);
    color: var(--text-primary);
    background: var(--surface-raised);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
  }
  .field input:focus {
    outline: none;
    border-color: var(--accent);
  }

  /* the quiet option, kept away from the two that answer the question now. */
  .dismiss {
    margin-inline-end: auto;
    padding: var(--space-2) 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    background: transparent;
    border: none;
    cursor: var(--cursor-action);
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .dismiss:hover {
    color: var(--text-primary);
  }

  .cancel {
    padding: var(--space-2) var(--space-4);
    font-size: var(--fz-label);
    color: var(--text-secondary);
    background: transparent;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    cursor: pointer;
  }
  .cancel:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .go {
    padding: var(--space-2) var(--space-4);
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    color: var(--accent-fg);
    background: var(--accent);
    border: none;
    border-radius: var(--radius-control);
    cursor: pointer;
  }
  .go:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
