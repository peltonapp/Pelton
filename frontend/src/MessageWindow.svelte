<script lang="ts">
  // MessageWindow is the root of a message opened in a window of its own
  // (#573, #298): the reading pane alone, filling the window. main.ts mounts it
  // instead of App when the window was opened with ?window=message. It shares
  // the backend with the main window but none of its state, so it loads the
  // preferences it needs to look and behave the same.
  import { onMount } from 'svelte'
  import MessageDetail from './components/detail/MessageDetail.svelte'
  import Compose from './components/compose/Compose.svelte'
  import Toasts from './components/common/Toasts.svelte'
  import ContextMenu from './components/common/ContextMenu.svelte'
  import ConfirmDialog from './components/common/ConfirmDialog.svelte'
  import AttachmentPreview from './components/detail/AttachmentPreview.svelte'
  import MoveDialog from './components/detail/MoveDialog.svelte'
  import SnoozeDialog from './components/detail/SnoozeDialog.svelte'
  import { initPrefs } from './stores/prefs'
  import { initComposePrefs, composeSessions } from './stores/compose'
  import { loadSignatures } from './stores/signatures'
  import { loadVIPSenders } from './stores/vip'
  import { loadVirusTotalConfig } from './stores/virustotal'
  import { openMessageId } from './stores/selection'
  import { closeCurrentWindow, titleBarDoubleClick } from './lib/api'
  import { isMac } from './lib/i18n'

  /** The message this window shows. */
  export let messageId: number

  let ready = false

  onMount(async () => {
    if (isMac) {
      document.documentElement.dataset.titlebar = 'mac'
    }
    await initPrefs()
    await initComposePrefs()
    void loadSignatures()
    void loadVIPSenders()
    void loadVirusTotalConfig()
    openMessageId.set(messageId)
    ready = true
  })

  // a message deleted, archived or moved from here is gone from this window's
  // point of view (the reading pane moves on or empties), and an empty window
  // is no use, so it closes.
  $: if (ready && $openMessageId !== messageId) {
    closeCurrentWindow()
  }

  // Cmd/Ctrl+W closes this window. On macOS the native menu already does it;
  // Windows and Linux have no menu bar in a message window.
  function onKeydown(event: KeyboardEvent): void {
    if (!isMac && event.ctrlKey && !event.shiftKey && !event.altKey && event.key.toLowerCase() === 'w') {
      event.preventDefault()
      closeCurrentWindow()
    }
  }
</script>

<svelte:window on:keydown={onKeydown} />

{#if isMac}
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div
    class="titlebar-drag"
    style="--wails-draggable:drag"
    aria-hidden="true"
    on:dblclick={titleBarDoubleClick}
  ></div>
{/if}

<div class="message-window" class:mac={isMac}>
  {#if ready}
    <MessageDetail />
  {/if}
</div>

{#if $composeSessions.length > 0}
  <div class="compose-layer">
    {#each $composeSessions as session (session.id)}
      <Compose {session} />
    {/each}
  </div>
{/if}

<Toasts />
<ContextMenu />
<ConfirmDialog />
<SnoozeDialog />
<AttachmentPreview />
<MoveDialog />

<style>
  .message-window {
    display: grid;
    grid-template-rows: minmax(0, 1fr);
    height: 100%;
    width: 100%;
    overflow: hidden;
  }

  /* the hidden macOS title bar: keep the strip under the traffic lights
     clear, as the main window does. */
  .message-window.mac {
    padding-top: var(--titlebar-lights);
  }

  .titlebar-drag {
    position: fixed;
    inset: 0 0 auto 0;
    height: var(--titlebar-lights);
    z-index: 700;
  }

  .compose-layer {
    position: fixed;
    bottom: var(--space-5);
    right: var(--space-5);
    left: var(--space-5);
    display: flex;
    flex-wrap: wrap-reverse;
    justify-content: flex-end;
    gap: var(--space-4);
    align-items: flex-end;
    z-index: 90;
    pointer-events: none;
  }

  .compose-layer > :global(.compose) {
    pointer-events: auto;
  }
</style>
