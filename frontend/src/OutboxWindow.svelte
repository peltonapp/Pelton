<script lang="ts">
  // OutboxWindow is the root of the send activity window: the outbox with its
  // actions beside the live send log. main.ts mounts it for ?window=outbox.
  // The bar at the top is the window's title bar, laid out the way a compose
  // window's toolbar is, so it drags the window and sits beside the macOS
  // traffic lights.
  import { onDestroy, onMount } from 'svelte'
  import OutboxList from './components/shell/OutboxList.svelte'
  import OutboxLog from './components/shell/OutboxLog.svelte'
  import Toasts from './components/common/Toasts.svelte'
  import { initPrefs, prefs } from './stores/prefs'
  import { initComposePrefs, openRequestSession, getSession, closeCompose } from './stores/compose'
  import { outbox, loadOutbox, quietOutbox } from './stores/outbox'
  import { outboxLog, followOutboxLog } from './stores/outboxlog'
  import { errorMessage, toastError } from './stores/toast'
  import { setRequestOpener } from './lib/outboxactions'
  import { applyTrafficLights, closeCurrentWindow, openComposeWindow, setWindowTitle, titleBarDoubleClick } from './lib/api'
  import { onOutboxChanged, type Unsubscribe } from './lib/events'
  import { isMac, t } from './lib/i18n'

  /** The token TrafficLights knows this window by. */
  const lightsToken = 'outbox'

  let now = Date.now()
  const timer = setInterval(() => (now = Date.now()), 30000)
  const stops: Unsubscribe[] = []
  onDestroy(() => {
    clearInterval(timer)
    stops.forEach((stop) => stop())
  })

  // a message taken back from here has no pane to open in, so it gets a
  // compose window of its own.
  setRequestOpener(async (req) => {
    const id = openRequestSession(req)
    const session = getSession(id)
    try {
      await openComposeWindow(session)
      closeCompose(id)
    } catch (err) {
      toastError(errorMessage(err))
    }
  })

  onMount(async () => {
    quietOutbox()
    if (isMac) {
      document.documentElement.dataset.titlebar = 'mac'
    }
    await initPrefs()
    if (isMac && $prefs.design !== 'classic') {
      void applyTrafficLights(lightsToken, true)
    }
    void initComposePrefs()
    stops.push(onOutboxChanged(() => void loadOutbox()))
    stops.push(followOutboxLog())
    await loadOutbox()
  })

  $: setWindowTitle($t('outbox.window.title'))

  // Cmd/Ctrl+W closes this window. On macOS the native menu already does it.
  function onKeydown(event: KeyboardEvent): void {
    if (!isMac && event.ctrlKey && !event.shiftKey && !event.altKey && event.key.toLowerCase() === 'w') {
      event.preventDefault()
      closeCurrentWindow()
    }
  }
</script>

<svelte:window on:keydown={onKeydown} />

<div class="outbox-window">
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <header class="bar" style="--wails-draggable:drag" on:dblclick|self={titleBarDoubleClick}>
    <h1>{$t('outbox.window.title')}</h1>
  </header>

  <div class="panes">
    <section class="queue" aria-labelledby="outbox-queue-title">
      <h2 id="outbox-queue-title">{$t('outbox.window.queue')}</h2>
      <div class="scroll">
        <OutboxList rows={$outbox} {now} />
      </div>
    </section>
    <div class="log">
      <OutboxLog entries={$outboxLog} />
    </div>
  </div>
</div>

<Toasts />

<style>
  .outbox-window {
    position: fixed;
    inset: 0;
    display: grid;
    grid-template-rows: auto minmax(0, 1fr);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-family: var(--font-ui);
  }

  /* the window's title bar: as tall as the bar the traffic lights are centred
     in, and clear of them, like a compose window's toolbar. */
  .bar {
    display: flex;
    align-items: center;
    min-height: max(calc(var(--control-height) + 2 * var(--space-2)), var(--titlebar-lights));
    padding: 0 var(--pane-pad) 0 max(var(--pane-pad), var(--titlebar-inset));
    border-bottom: var(--hairline) solid var(--border-default);
    background: var(--surface-sunken);
    user-select: none;
  }

  h1 {
    margin: 0;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
  }

  .panes {
    display: grid;
    grid-template-columns: minmax(0, var(--popover-width)) minmax(0, 1fr);
    min-height: 0;
  }

  .queue {
    display: flex;
    flex-direction: column;
    min-height: 0;
    border-right: var(--hairline) solid var(--border-subtle);
  }

  h2 {
    display: flex;
    align-items: center;
    min-height: var(--control-height);
    margin: 0;
    padding: 0 var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    color: var(--text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-2) 0;
  }

  .log {
    min-height: 0;
    min-width: 0;
  }
</style>
