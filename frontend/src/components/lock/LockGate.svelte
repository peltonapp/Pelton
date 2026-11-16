<script lang="ts">
  // LockGate wraps what every window shows: its content, or the lock screen
  // while the app lock is engaged. In the main window the content is taken
  // out of the page while locked and mounted again after, so nothing of the
  // mail is left behind the lock screen; the stores keep the session in
  // between. A secondary window (a compose, a message) is hidden by the
  // backend while locked and keeps its content, so a draft survives; one
  // opened while locked shows the lock screen and mounts its content only
  // once unlocked. Every window also reports input for the
  // lock's idle time.
  import { onDestroy, onMount } from 'svelte'
  import LockScreen from './LockScreen.svelte'
  import { getLockState, noteActivity } from '../../lib/api'
  import { onAppLock } from '../../lib/events'
  import { watchActivity } from '../../lib/activity'
  import { setLocale } from '../../lib/i18n'
  import { applyTheme } from '../../theme/theme'
  import { applyAccent } from '../../theme/accent'
  import { frameIntegrated, refreshWindowFrame } from '../../stores/windowframe'
  import { isMac } from '../../lib/platform'
  import type { LockState, ThemePref } from '../../lib/types'

  /** Set for the main window, whose content leaves the page while locked. */
  export let main = false
  /** The lock state read before mounting, or null when it could not be read. */
  export let initial: LockState | null = null

  let state: LockState | null = initial
  let locked = initial?.locked ?? false
  // whether the content has ever been mounted; a window opened while locked
  // waits for the unlock.
  let mounted = !locked
  // the main window's content is mounting again after an unlock.
  let resumed = false

  // a window that boots locked has no preferences loaded, so the lock screen
  // takes the theme, accent and language from the lock state instead.
  function dress(s: LockState): void {
    if (document.documentElement.hasAttribute('data-theme')) {
      return
    }
    applyTheme(s.theme as ThemePref)
    applyAccent(s.accent)
    setLocale(s.language)
  }

  async function lock(): Promise<void> {
    try {
      state = await getLockState()
    } catch {
      // keep the last known state; the backend refuses everything else anyway.
    }
    if (state) {
      dress(state)
    }
    locked = true
  }

  function unlock(): void {
    if (!locked) {
      return
    }
    if (main && mounted) {
      resumed = true
    }
    locked = false
    mounted = true
  }

  const stopLock = onAppLock((e) => {
    if (e.locked) {
      void lock()
    } else {
      unlock()
    }
  })
  const stopActivity = watchActivity(() => {
    if (!locked) {
      void noteActivity().catch(() => {})
    }
  })

  onMount(() => {
    if (locked && state) {
      dress(state)
    }
    if (main && locked && !isMac) {
      void refreshWindowFrame()
    }
  })

  onDestroy(() => {
    stopLock()
    stopActivity()
  })
</script>

{#if main}
  {#if locked}
    {#if state}
      <LockScreen {state} windowButtons={!isMac && $frameIntegrated} on:unlocked={unlock} />
    {/if}
  {:else}
    <slot {resumed} />
  {/if}
{:else}
  {#if locked && state}
    <LockScreen {state} on:unlocked={unlock} />
  {/if}
  {#if mounted}
    <div class="held" class:hidden={locked}>
      <slot {resumed} />
    </div>
  {/if}
{/if}

<style>
  .held {
    display: contents;
  }

  .held.hidden {
    display: none;
  }
</style>
