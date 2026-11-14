<script lang="ts">
  // the rail: a slim strip on one window edge that switches between mail,
  // contacts and the calendar, with search, the profile and settings at its
  // foot. a marker glides to the active entry.
  import { createEventDispatcher } from 'svelte'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
  import settingsIcon from '@phosphor-icons/core/assets/duotone/gear-six-duotone.svg?raw'
  import userIcon from '@phosphor-icons/core/assets/duotone/user-circle-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { spaces, spaceDrop } from '../../lib/spaces'
  import { maildrop } from '../../stores/maildrag'
  import { space, openSpace } from '../../stores/space'
  import { currentProfile, profiles, switchRelative } from '../../stores/profiles'
  import { tip } from '../../lib/tip'
  import { t } from '../../lib/i18n'
  import type { RailSide } from '../../lib/types'

  /** Which window edge the rail is on; tooltips open toward the content. */
  export let side: RailSide = 'left'
  /** Unread count of the unified inbox, shown as a dot on Mail. */
  export let unread: number = 0

  const dispatch = createEventDispatcher<{ search: void; settings: void }>()

  $: tipSide = side === 'left' ? ('right' as const) : ('left' as const)
  $: activeIndex = Math.max(0, spaces.findIndex((s) => s.key === $space))
</script>

<nav class="rail" class:right={side === 'right'} aria-label={$t('space.switcher')} style="--wails-draggable:drag">
  <div class="group" style={`--active:${activeIndex}`}>
    <span class="marker" aria-hidden="true"></span>
    {#each spaces as entry (entry.key)}
      <button
        type="button"
        class="entry"
        class:active={$space === entry.key}
        aria-label={$t(entry.labelKey)}
        aria-current={$space === entry.key ? 'page' : undefined}
        use:tip={{ text: $t(entry.labelKey), side: tipSide }}
        use:maildrop={spaceDrop(entry.key)}
        on:click={() => openSpace(entry.key)}
      >
        <Ph svg={entry.icon} size={19} />
        {#if entry.key === 'mail' && unread > 0}
          <span class="badge" aria-hidden="true"></span>
        {/if}
      </button>
    {/each}
  </div>

  <div class="group foot">
    <button
      type="button"
      class="entry"
      aria-label={$t('shortcut.search')}
      use:tip={{ text: $t('shortcut.search'), side: tipSide }}
      on:click={() => dispatch('search')}
    >
      <Ph svg={searchIcon} size={19} />
    </button>
    {#if $profiles.length > 1}
      <button
        type="button"
        class="entry profile"
        aria-label={$t('profiles.current').replace('{name}', $currentProfile?.name ?? '')}
        use:tip={{ text: $t('profiles.current').replace('{name}', $currentProfile?.name ?? ''), side: tipSide }}
        on:click={() => void switchRelative(1)}
      >
        {#if $currentProfile?.icon}
          <span class="glyph">{$currentProfile.icon}</span>
        {:else}
          <Ph svg={userIcon} size={19} />
        {/if}
      </button>
    {/if}
    <button
      type="button"
      class="entry"
      aria-label={$t('settings.title')}
      use:tip={{ text: $t('settings.title'), side: tipSide }}
      on:click={() => dispatch('settings')}
    >
      <Ph svg={settingsIcon} size={19} />
    </button>
  </div>
</nav>

<style>
  .rail {
    --entry: calc(var(--rail-width) - var(--space-3));
    display: flex;
    flex-direction: column;
    justify-content: space-between;
    align-items: center;
    width: var(--rail-width);
    padding: var(--space-2) 0 var(--space-3);
    min-height: 0;
  }

  .group {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    --wails-draggable: no-drag;
  }

  .entry {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--entry);
    height: var(--entry);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition:
      color var(--motion-fast) var(--ease-out),
      background-color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .entry:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .entry:active {
    transform: scale(0.94);
  }

  .entry.active {
    color: var(--accent);
  }

  /* the marker sits on the window edge beside the active entry and slides
     between entries rather than jumping. */
  .marker {
    position: absolute;
    inset-inline-start: calc((var(--entry) - var(--rail-width)) / 2);
    top: calc(var(--entry) * 0.25);
    width: 2.5px;
    height: calc(var(--entry) * 0.5);
    border-radius: 0 2px 2px 0;
    background: var(--accent);
    transform: translateY(calc(var(--active) * (var(--entry) + var(--space-2))));
    transition: transform var(--motion-base) var(--ease-out);
  }

  .rail.right .marker {
    inset-inline-start: auto;
    inset-inline-end: calc((var(--entry) - var(--rail-width)) / 2);
    border-radius: 2px 0 0 2px;
  }

  .badge {
    position: absolute;
    top: 5px;
    inset-inline-end: 5px;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent);
    box-shadow: 0 0 0 1.5px var(--chrome-bg);
  }

  .glyph {
    font-size: var(--fz-heading);
    line-height: 1;
  }
</style>
