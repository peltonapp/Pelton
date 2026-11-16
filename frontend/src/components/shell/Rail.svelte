<script lang="ts">
  // the rail: a slim strip on one window edge that switches between the
  // spaces, with search, the profile and settings at its
  // foot. a marker glides to the active entry.
  import { createEventDispatcher } from 'svelte'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
  import settingsIcon from '@phosphor-icons/core/assets/duotone/gear-six-duotone.svg?raw'
  import userIcon from '@phosphor-icons/core/assets/duotone/user-circle-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { spaces, spaceDrop } from '../../lib/spaces'
  import { maildrop } from '../../stores/maildrag'
  import { space, openSpace } from '../../stores/space'
  import { taskBadge } from '../../stores/tasks'
  import { feedsUnread } from '../../stores/feeds'
  import { currentProfile, profiles, switchRelative } from '../../stores/profiles'
  import { tip } from '../../lib/tip'
  import { t } from '../../lib/i18n'
  import type { ComponentType } from 'svelte'
  import { IconPinnedOff } from '@tabler/icons-svelte'
  import type { RailSide, ViewKey } from '../../lib/types'
  import { prefs, railPinIds, toggleRailPin } from '../../stores/prefs'
  import { sidebar } from '../../stores/accounts'
  import { views } from '../../stores/views'
  import { selection, selectView, selectSavedView, unifiedViewLabel } from '../../stores/selection'
  import { unifiedViewIcon } from '../../lib/unifiedviewicons'
  import { viewIconComponent, viewColorCss } from '../../lib/viewicons'
  import { openContextMenu } from '../../stores/contextmenu'
  import { focus } from '../../stores/focus'

  /** Which window edge the rail is on; tooltips open toward the content. */
  export let side: RailSide = 'left'
  /** Unread count of the unified inbox, shown as a dot on Mail. */
  export let unread: number = 0

  const dispatch = createEventDispatcher<{ search: void; settings: void }>()

  $: tipSide = side === 'left' ? ('right' as const) : ('left' as const)
  $: activeIndex = Math.max(0, spaces.findIndex((s) => s.key === $space))

  /** A view pinned to the middle of the rail. */
  interface Pin {
    id: string
    label: string
    icon: ComponentType
    color: string
    unread: boolean
    active: boolean
    open: () => void
  }

  // the pinned views, in the order they were pinned. one whose view is gone
  // (a deleted saved view) simply does not show.
  $: pins = railPinIds($prefs.railPins)
    .map((id): Pin | null => {
      const [kind, key] = [id.slice(0, 1), id.slice(2)]
      if (kind === 'u') {
        const view = ($sidebar.data?.views ?? []).find((v) => v.key === key)
        if (!view) {
          return null
        }
        const label = unifiedViewLabel(view.key)
        return {
          id,
          label,
          icon: unifiedViewIcon(view.key),
          color: '',
          unread: view.unreadCount > 0,
          active: $space === 'mail' && $selection.kind === 'view' && $selection.view === view.key,
          open: () => selectView(view.key as ViewKey, label),
        }
      }
      const view = $views.find((v) => String(v.id) === key)
      if (!view) {
        return null
      }
      return {
        id,
        label: view.name,
        icon: viewIconComponent(view.icon),
        color: viewColorCss(view.color),
        unread: view.unreadCount > 0,
        active: $space === 'mail' && $selection.kind === 'savedView' && $selection.viewId === view.id,
        open: () => selectSavedView(view.id, view.name),
      }
    })
    .filter((pin): pin is Pin => pin !== null)

  function openPin(pin: Pin): void {
    openSpace('mail')
    pin.open()
  }

  function onPinContext(event: MouseEvent, pin: Pin): void {
    openContextMenu(event.clientX, event.clientY, [
      { label: $t('rail.unpin'), icon: IconPinnedOff, action: () => toggleRailPin(pin.id) },
    ])
  }
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
        {#if entry.key === 'mail' && unread > 0 && !$focus.active}
          <span class="badge" aria-hidden="true"></span>
        {/if}
        {#if entry.key === 'feeds' && $feedsUnread > 0 && !$focus.active}
          <span class="badge" aria-hidden="true"></span>
        {/if}
        {#if entry.key === 'tasks' && $taskBadge > 0}
          <span class="count" aria-hidden="true">{$taskBadge > 99 ? '99+' : $taskBadge}</span>
        {/if}
      </button>
    {/each}
  </div>

  {#if pins.length > 0}
    <div class="group pins" aria-label={$t('rail.pins')}>
      {#each pins as pin (pin.id)}
        <button
          type="button"
          class="entry pin"
          class:active={pin.active}
          aria-label={pin.label}
          aria-current={pin.active ? 'page' : undefined}
          style={pin.color ? `--pin-color:${pin.color}` : ''}
          use:tip={{ text: pin.label, side: tipSide }}
          on:click={() => openPin(pin)}
          on:contextmenu|preventDefault={(e) => onPinContext(e, pin)}
        >
          <svelte:component this={pin.icon} size={17} stroke={1.6} />
          {#if pin.unread && !$focus.active}
            <span class="badge" aria-hidden="true"></span>
          {/if}
        </button>
      {/each}
    </div>
  {/if}

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

  /* the pinned views sit in the middle of the rail and scroll if there are
     more than fit, so the spaces above and the foot below always stay put. */
  .pins {
    flex: 0 1 auto;
    min-height: 0;
    margin-block: var(--space-4);
    padding-block: var(--space-3);
    border-block: var(--hairline) solid var(--border-subtle);
    overflow-y: auto;
    scrollbar-width: none;
  }

  .pin {
    flex-shrink: 0;
    color: var(--pin-color, var(--text-tertiary));
  }

  .pin.active {
    background: var(--selection-bg);
    color: var(--pin-color, var(--accent));
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

  /* the Tasks entry counts what is due, in the quiet text color: a number
     there is information, not an alarm. */
  .count {
    position: absolute;
    top: var(--space-1);
    inset-inline-end: var(--space-1);
    min-width: var(--space-5);
    padding: 0 var(--space-1);
    border-radius: var(--radius-control);
    background: var(--chrome-bg);
    box-shadow: 0 0 0 var(--hairline) var(--border-default);
    font-size: var(--fz-meta);
    line-height: 1.4;
    color: var(--text-secondary);
    font-variant-numeric: tabular-nums;
    text-align: center;
  }

  .glyph {
    font-size: var(--fz-heading);
    line-height: 1;
  }
</style>
