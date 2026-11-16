<script lang="ts">
  // Root picks what a window shows from its ?window= query, inside the lock
  // gate every window has: the app in the main window, or a message, a
  // compose or the send activity in a window of their own.
  import App from './App.svelte'
  import MessageWindow from './MessageWindow.svelte'
  import ComposeWindow from './ComposeWindow.svelte'
  import OutboxWindow from './OutboxWindow.svelte'
  import LockGate from './components/lock/LockGate.svelte'
  import type { LockState } from './lib/types'

  /** The ?window= value, or empty for the main window. */
  export let kind = ''
  /** The message a message window shows. */
  export let messageId = 0
  /** The handoff token a compose window takes its compose with. */
  export let composeToken = ''
  /** The lock state read before mounting, or null when it could not be read. */
  export let lockState: LockState | null = null

  $: view =
    kind === 'message' && messageId > 0
      ? 'message'
      : kind === 'compose' && composeToken
        ? 'compose'
        : kind === 'outbox'
          ? 'outbox'
          : 'main'
</script>

<LockGate main={view === 'main'} initial={lockState} let:resumed>
  {#if view === 'message'}
    <MessageWindow {messageId} />
  {:else if view === 'compose'}
    <ComposeWindow token={composeToken} />
  {:else if view === 'outbox'}
    <OutboxWindow />
  {:else}
    <App {resumed} />
  {/if}
</LockGate>
