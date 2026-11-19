<script lang="ts">
  // QuickComposeWindow is the root of the quick compose window, which the
  // system-wide shortcut or --quick-compose opens without the main window.
  // It holds one new message in the new composer's reduced layout: From, To,
  // Subject, the text and attachments, Send. Escape closes it, saving a draft
  // first when there is something in it; the window goes with its compose.
  import { onDestroy, onMount } from 'svelte'
  import { get } from 'svelte/store'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import Composer2 from './components/compose/Composer2.svelte'
  import Toasts from './components/common/Toasts.svelte'
  import ContextMenu from './components/common/ContextMenu.svelte'
  import ConfirmDialog from './components/common/ConfirmDialog.svelte'
  import Ph from './components/common/Ph.svelte'
  import { initPrefs, prefs } from './stores/prefs'
  import { initShortcuts } from './stores/shortcuts'
  import { initComposePrefs, composeSessions, composeInWindow, openCompose, requestComposeClose } from './stores/compose'
  import { loadSignatures } from './stores/signatures'
  import { loadSnippets } from './stores/snippets'
  import { loadSidebar, sidebar } from './stores/accounts'
  import { applyTrafficLights, closeCurrentWindow, setWindowTitle } from './lib/api'
  import { onComposeCloseRequest } from './lib/events'
  import { popupIsOpen } from './lib/popups'
  import { isMac, t } from './lib/i18n'
  import type { EditorMode } from './lib/types'

  let ready = false
  let noMailbox = false

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
    if (isMac) {
      void applyTrafficLights('quick', true)
    }
    await initComposePrefs()
    await initShortcuts()
    void loadSignatures()
    void loadSnippets()
    await loadSidebar()
    const accounts = get(sidebar).data?.accounts ?? []
    if (accounts.length === 0) {
      noMailbox = true
      return
    }
    openCompose(accounts[0].id, get(prefs).defaultEditorMode as EditorMode)
    ready = true
  })

  // the window goes with its compose: sent, saved and closed, or handed to
  // the full composer.
  $: if (ready && $composeSessions.length === 0) {
    closeCurrentWindow()
  }

  $: if (ready) {
    setWindowTitle($composeSessions[0]?.subject || $t('compose.title.untitled'))
  }

  // Escape closes, unless something inside took it first: an open list, a
  // suggestion, or vim's normal mode in the editor.
  function onKeydown(event: KeyboardEvent): void {
    if (event.key !== 'Escape' || event.defaultPrevented || popupIsOpen()) {
      return
    }
    if ($prefs.composeVimMode && (event.target as HTMLElement | null)?.closest?.('.editor')) {
      return
    }
    const session = $composeSessions[0]
    if (session) {
      event.preventDefault()
      requestComposeClose(session.id)
    }
  }
</script>

<svelte:window on:keydown={onKeydown} />

{#if noMailbox}
  <div class="empty" style="--wails-draggable:drag">
    <p>{$t('quickCompose.noMailbox')}</p>
    <button type="button" aria-label={$t('compose.window.close')} on:click={closeCurrentWindow}>
      <Ph svg={xIcon} size={14} />
      <span>{$t('compose.window.close')}</span>
    </button>
  </div>
{/if}

{#each $composeSessions as session (session.id)}
  <Composer2 {session} layout="quick" />
{/each}

<Toasts />
<ContextMenu />
<ConfirmDialog />

<style>
  .empty {
    position: fixed;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-4);
    background: var(--surface-raised);
    color: var(--text-secondary);
    font-family: var(--font-ui);
    font-size: var(--fz-list);
  }

  .empty p {
    margin: 0;
  }

  button {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--control-height);
    padding: 0 var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    --wails-draggable: no-drag;
  }

  button:hover {
    background: var(--surface-hover);
  }
</style>
