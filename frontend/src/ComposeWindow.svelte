<script lang="ts">
  // ComposeWindow is the root of a compose moved into a window of its own
  // (#573). main.ts mounts it for ?window=compose&token=..., and the token
  // fetches the pane, with everything typed so far, from the backend once.
  // The window lives as long as its compose: sending, saving or discarding
  // closes it, and its close button asks the way the pane's own button does.
  import { onDestroy, onMount } from 'svelte'
  import Compose from './components/compose/Compose.svelte'
  import Toasts from './components/common/Toasts.svelte'
  import ContextMenu from './components/common/ContextMenu.svelte'
  import ConfirmDialog from './components/common/ConfirmDialog.svelte'
  import { initPrefs } from './stores/prefs'
  import {
    initComposePrefs,
    composeSessions,
    composeInWindow,
    adoptSession,
    requestComposeClose,
    type ComposeSession,
  } from './stores/compose'
  import { loadSignatures } from './stores/signatures'
  import { loadSidebar } from './stores/accounts'
  import { closeCurrentWindow, setWindowTitle, takeComposeSession, titleBarDoubleClick } from './lib/api'
  import { onComposeCloseRequest } from './lib/events'
  import { isMac, t } from './lib/i18n'

  /** The handoff token the window was opened with. */
  export let token: string

  let ready = false
  const stopCloseRequests = onComposeCloseRequest(() => {
    const session = $composeSessions[0]
    if (session) {
      requestComposeClose(session.id)
    } else {
      closeCurrentWindow()
    }
  })
  onDestroy(stopCloseRequests)

  onMount(async () => {
    composeInWindow.set(true)
    if (isMac) {
      document.documentElement.dataset.titlebar = 'mac'
    }
    await initPrefs()
    await initComposePrefs()
    void loadSignatures()
    // the account picker and the From line read the accounts from here.
    await loadSidebar()
    const session = await takeComposeSession<ComposeSession>(token).catch(() => null)
    if (!session) {
      closeCurrentWindow()
      return
    }
    adoptSession(session)
    ready = true
  })

  // the window goes with its compose.
  $: if (ready && $composeSessions.length === 0) {
    closeCurrentWindow()
  }

  $: if (ready) {
    setWindowTitle($composeSessions[0]?.subject || $t('compose.title.untitled'))
  }
</script>

{#if isMac}
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div
    class="titlebar-drag"
    style="--wails-draggable:drag"
    aria-hidden="true"
    on:dblclick={titleBarDoubleClick}
  ></div>
{/if}

{#each $composeSessions as session (session.id)}
  <Compose {session} />
{/each}

<Toasts />
<ContextMenu />
<ConfirmDialog />

<style>
  .titlebar-drag {
    position: fixed;
    inset: 0 0 auto 0;
    height: var(--titlebar-lights);
    z-index: 700;
  }
</style>
