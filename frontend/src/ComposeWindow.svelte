<script lang="ts">
  // ComposeWindow is the root of a compose moved into a window of its own
  // (#573). main.ts mounts it for ?window=compose&token=..., and the token
  // fetches the pane, with everything typed so far, from the backend once.
  // The window lives as long as its compose: sending, saving or discarding
  // closes it, and its close button asks the way the pane's own button does.
  import { onDestroy, onMount } from 'svelte'
  import Compose from './components/compose/Compose.svelte'
  import Composer2 from './components/compose/Composer2.svelte'
  import Toasts from './components/common/Toasts.svelte'
  import ContextMenu from './components/common/ContextMenu.svelte'
  import ConfirmDialog from './components/common/ConfirmDialog.svelte'
  import CommandPalette from './components/common/CommandPalette.svelte'
  import { initPrefs, prefs } from './stores/prefs'
  import { bindings, initShortcuts, recording } from './stores/shortcuts'
  import { initPalette, paletteOpen, togglePalette } from './stores/palette'
  import { extraPaletteCommands } from './lib/palettesources'
  import { comboMatches } from './lib/shortcuts'
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
  import { applyTrafficLights, closeCurrentWindow, setWindowTitle, takeComposeSession, titleBarDoubleClick } from './lib/api'
  import { onComposeCloseRequest } from './lib/events'
  import { isMac, t } from './lib/i18n'

  /** The handoff token the window was opened with. */
  export let token: string

  let ready = false
  $: classic = $prefs.design === 'classic'
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
    if (isMac && $prefs.design !== 'classic') {
      // the composer's toolbar is this window's title bar, so it is sized to
      // put the window buttons on its centre line.
      void applyTrafficLights(token, true)
    }
    await initComposePrefs()
    await initShortcuts()
    void initPalette()
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

  // the palette here holds what a compose window can do: the composer's own
  // actions, registered by the composer.
  function onKeydown(event: KeyboardEvent): void {
    const combo = $bindings['command-palette']
    if (classic || $recording || !combo || !comboMatches(event, combo)) {
      return
    }
    event.preventDefault()
    togglePalette()
  }
</script>

<svelte:window on:keydown={onKeydown} />

<!-- the new composer's toolbar is the title bar of its window, so only the
     classic pane needs the strip. -->
{#if isMac && classic}
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div
    class="titlebar-drag"
    style="--wails-draggable:drag"
    aria-hidden="true"
    on:dblclick={titleBarDoubleClick}
  ></div>
{/if}

{#each $composeSessions as session (session.id)}
  {#if classic}
    <Compose {session} />
  {:else}
    <Composer2 {session} layout="window" />
  {/if}
{/each}

<Toasts />
<ContextMenu />
<ConfirmDialog />
{#if !classic}
  <CommandPalette commands={$paletteOpen ? extraPaletteCommands($t) : []} />
{/if}

<style>
  .titlebar-drag {
    position: fixed;
    inset: 0 0 auto 0;
    height: var(--titlebar-lights);
    z-index: 700;
  }
</style>
