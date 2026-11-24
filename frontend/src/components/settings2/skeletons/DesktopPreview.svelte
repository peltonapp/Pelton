<script lang="ts">
  // a bit of desktop: the system bar along the top with Pelton's tray icon, the
  // app window, and a banner for new mail. each part is drawn only when asked
  // for, so the picture can show the app running without a window, or a new
  // message announcing itself over whatever else is open.
  import Frame from './Frame.svelte'
  import MiniWindow from './MiniWindow.svelte'
  import { sampleMessages } from './samples'

  /** Draws the app window. */
  export let appWindow: boolean = true
  /** Draws Pelton's icon in the system bar. */
  export let tray: boolean = true
  /** Draws a new mail banner in the corner. */
  export let banner: boolean = false

  const message = sampleMessages[0]
</script>

<Frame sunken>
  <div class="screen">
    <div class="bar">
      <span class="tray">
        <i></i>
        {#if tray}
          <i class="app"></i>
        {/if}
        <i></i>
      </span>
    </div>
    {#if appWindow}
      <div class="app-window"><MiniWindow /></div>
    {/if}
    {#if banner}
      <div class="banner">
        <span class="icon"></span>
        <span class="text">
          <span class="from">{message.name}</span>
          <span class="subject">{message.subject}</span>
        </span>
      </div>
    {/if}
  </div>
</Frame>

<style>
  .screen {
    position: relative;
    height: 120px;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    background: var(--surface-base);
    overflow: hidden;
  }

  .bar {
    display: flex;
    justify-content: flex-end;
    padding: var(--space-1) var(--space-3);
    background: var(--surface-hover);
  }

  .tray {
    display: inline-flex;
    gap: var(--space-3);
  }

  .tray i {
    width: 8px;
    height: 8px;
    border-radius: 2px;
    background: var(--border-strong);
  }

  .tray i.app {
    background: var(--accent);
  }

  .app-window {
    position: absolute;
    inset: 28px 14% 8px;
    border: var(--hairline) solid var(--border-strong);
    border-radius: var(--radius-control);
    box-shadow: var(--shadow-overlay);
    overflow: hidden;
  }

  .banner {
    position: absolute;
    top: 22px;
    inset-inline-end: var(--space-3);
    display: flex;
    align-items: center;
    gap: var(--space-3);
    width: 60%;
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    animation: rise var(--motion-base) var(--ease-out);
  }

  .icon {
    width: 20px;
    height: 20px;
    flex-shrink: 0;
    border-radius: var(--radius-control);
    background: var(--accent);
  }

  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    font-size: var(--fz-meta);
  }

  .from {
    color: var(--text-primary);
    font-weight: var(--fw-medium);
  }

  .subject {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-tertiary);
  }

  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
  }
</style>
