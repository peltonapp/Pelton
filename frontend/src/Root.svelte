<script context="module" lang="ts">
  import type { ComponentType, SvelteComponent } from 'svelte'
  import { withStrings } from './lib/lazyload'

  /** What a window shows: the app, or one of the windows of their own. */
  export type WindowView = 'main' | 'message' | 'compose' | 'outbox' | 'quick'

  /** The component of one view. Each takes its own props. */
  export type WindowComponent = ComponentType<SvelteComponent<any>>

  /** windowView picks what a window shows from its ?window= query. */
  export function windowView(kind: string, messageId: number, composeToken: string): WindowView {
    if (kind === 'message' && messageId > 0) {
      return 'message'
    }
    if (kind === 'compose' && composeToken) {
      return 'compose'
    }
    if (kind === 'outbox' || kind === 'quick') {
      return kind
    }
    return 'main'
  }

  /**
   * loadWindow loads the component of one view, so a window loads its own code
   * and not the code of every other kind of window.
   */
  export async function loadWindow(view: WindowView): Promise<WindowComponent> {
    switch (view) {
      case 'message':
        return (await loadMessageWindow()).default
      case 'compose':
        return (await loadComposeWindow()).default
      case 'outbox':
        return (await loadOutboxWindow()).default
      case 'quick':
        return (await loadQuickComposeWindow()).default
      default:
        return (await import('./App.svelte')).default
    }
  }

  const loadMessageWindow = withStrings(['composer'], () => import('./MessageWindow.svelte'))
  const loadComposeWindow = withStrings(['composer'], () => import('./ComposeWindow.svelte'))
  const loadOutboxWindow = withStrings(['outbox'], () => import('./OutboxWindow.svelte'))
  const loadQuickComposeWindow = withStrings(['composer'], () => import('./QuickComposeWindow.svelte'))
</script>

<script lang="ts">
  // Root shows a window's view inside the lock gate every window has: the app
  // in the main window, or a message, a compose, the send activity or quick
  // compose in a window of their own.
  import LockGate from './components/lock/LockGate.svelte'
  import type { LockState } from './lib/types'

  /** What this window shows, from windowView. */
  export let view: WindowView = 'main'
  /** The view's component, from loadWindow. */
  export let component: WindowComponent
  /** The message a message window shows. */
  export let messageId = 0
  /** The handoff token a compose window takes its compose with. */
  export let composeToken = ''
  /** The lock state read before mounting, or null when it could not be read. */
  export let lockState: LockState | null = null
</script>

<LockGate main={view === 'main'} initial={lockState} let:resumed>
  {#if view === 'message'}
    <svelte:component this={component} {messageId} />
  {:else if view === 'compose'}
    <svelte:component this={component} token={composeToken} />
  {:else if view === 'main'}
    <svelte:component this={component} {resumed} />
  {:else}
    <svelte:component this={component} />
  {/if}
</LockGate>
