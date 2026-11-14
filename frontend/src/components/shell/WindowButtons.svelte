<script lang="ts">
  // minimise, maximise and close for a window whose title bar is the app's own
  // top bar, on Windows and Linux. macOS keeps its traffic lights. the buttons
  // take the shape each platform's own title bar gives them: full-height flat
  // cells on Windows, small round ones on Linux.
  import { onMount } from 'svelte'
  import minimiseIcon from '@phosphor-icons/core/assets/regular/minus.svg?raw'
  import maximiseIcon from '@phosphor-icons/core/assets/regular/square.svg?raw'
  import restoreIcon from '@phosphor-icons/core/assets/regular/copy.svg?raw'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { minimiseWindow, toggleMaximiseWindow, isWindowMaximised, closeWindow } from '../../lib/api'
  import { isWindows, t } from '../../lib/i18n'

  let maximised = false

  async function refresh(): Promise<void> {
    maximised = await isWindowMaximised().catch(() => false)
  }

  async function toggle(): Promise<void> {
    await toggleMaximiseWindow().catch(() => undefined)
    await refresh()
  }

  onMount(refresh)
</script>

<svelte:window on:resize={refresh} />

<div class="buttons" class:windows={isWindows}>
  <button type="button" aria-label={$t('window.minimise')} on:click={minimiseWindow}>
    <Ph svg={minimiseIcon} size={14} />
  </button>
  <button type="button" aria-label={maximised ? $t('window.restore') : $t('window.maximise')} on:click={toggle}>
    <Ph svg={maximised ? restoreIcon : maximiseIcon} size={13} />
  </button>
  <button type="button" class="close" aria-label={$t('window.close')} on:click={closeWindow}>
    <Ph svg={closeIcon} size={14} />
  </button>
</div>

<style>
  .buttons {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    align-self: stretch;
    padding-inline-start: var(--space-3);
    --wails-draggable: no-drag;
  }

  button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    border: none;
    border-radius: 50%;
    background: var(--surface-hover);
    color: var(--text-secondary);
    cursor: default;
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  button:hover {
    background: var(--border-default);
    color: var(--text-primary);
  }

  .windows {
    gap: 0;
    padding-inline-start: var(--space-2);
  }

  .windows button {
    width: 46px;
    height: 100%;
    border-radius: 0;
    background: transparent;
  }

  .windows button:hover {
    background: var(--surface-hover);
  }

  .windows button.close:hover {
    background: var(--danger);
    color: var(--text-inverse);
  }
</style>
