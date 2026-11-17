<script lang="ts">
  // the rail: a slim strip on one window edge that switches between the
  // spaces, with search, the profile and settings at its
  // foot. a marker glides to the active entry. the spaces can be dragged into
  // a new order, and fold into a "more" entry when the window is too short
  // for all of them.
  import { createEventDispatcher, onDestroy, onMount, tick } from 'svelte'
  import { flip } from 'svelte/animate'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
  import settingsIcon from '@phosphor-icons/core/assets/duotone/gear-six-duotone.svg?raw'
  import userIcon from '@phosphor-icons/core/assets/duotone/user-circle-duotone.svg?raw'
  import moreIcon from '@phosphor-icons/core/assets/duotone/dots-three-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { navSpaces as spaces, visibleSpaces, spaceDrop, spaceMenu, moveSpaceShown, reorderShownSpaces, type NavEntry } from '../../lib/spaces'
  import { reorder, type ReorderDetail } from '../../lib/reorder'
  import { dur, motion } from '../../lib/motion'
  import { chatUnreadRooms, chatHasHighlight } from '../../stores/chat'
  import { maildrop } from '../../stores/maildrag'
  import { space, openSpace } from '../../stores/space'
  import { taskBadge } from '../../stores/tasks'
  import { feedsUnread } from '../../stores/feeds'
  import { currentProfile, profiles, switchRelative } from '../../stores/profiles'
  import { tip } from '../../lib/tip'
  import { t, shortcutLabel } from '../../lib/i18n'
  import { bindings } from '../../stores/shortcuts'
  import type { ShortcutAction } from '../../lib/shortcuts'
  import type { ComponentType } from 'svelte'
  import { IconPinnedOff } from '@tabler/icons-svelte'
  import type { RailSide, Space, ViewKey } from '../../lib/types'
  import { prefs, railPinIds, toggleRailPin } from '../../stores/prefs'
  import { sidebar } from '../../stores/accounts'
  import { views } from '../../stores/views'
  import { selection, selectView, selectSavedView, unifiedViewLabel } from '../../stores/selection'
  import { unifiedViewIcon } from '../../lib/unifiedviewicons'
  import { viewIconComponent, viewColorCss } from '../../lib/viewicons'
  import { openContextMenu } from '../../stores/contextmenu'
  import { focus } from '../../stores/focus'
  import { whatsNewUnseen } from '../../stores/whatsnew'

  /** Which window edge the rail is on; tooltips open toward the content. */
  export let side: RailSide = 'left'
  /** Unread count of the unified inbox, shown as a dot on Mail. */
  export let unread: number = 0

  const dispatch = createEventDispatcher<{ search: void; settings: void }>()

  $: tipSide = side === 'left' ? ('right' as const) : ('left' as const)

  // the keys bound to an action, as the tooltip shows them; nothing when unbound.
  $: keysFor = (action: string): string | undefined => {
    const combo = $bindings[action as ShortcutAction]
    return combo ? shortcutLabel(combo) : undefined
  }
  // --- folding -----------------------------------------------------------------
  // how many entries the spaces group has room for; past that, the last slot
  // becomes a "more" entry holding the rest.
  let railEl: HTMLElement
  let groupEl: HTMLDivElement
  let footEl: HTMLDivElement
  let pinsEl: HTMLDivElement | undefined
  let fit = Infinity

  function px(value: string): number {
    return parseFloat(value) || 0
  }

  function measure(): void {
    const sample = groupEl?.querySelector<HTMLElement>('.entry')
    const size = sample?.offsetHeight ?? 0
    if (!railEl || !footEl || size <= 0) {
      fit = Infinity
      return
    }
    const gap = px(getComputedStyle(groupEl).rowGap)
    const rail = getComputedStyle(railEl)
    let reserve = footEl.offsetHeight + px(rail.paddingTop) + px(rail.paddingBottom)
    // the pinned views keep room for one entry and their frame.
    if (pinsEl) {
      const p = getComputedStyle(pinsEl)
      reserve += size + px(p.marginTop) + px(p.marginBottom) + px(p.paddingTop) + px(p.paddingBottom) + px(p.borderTopWidth) + px(p.borderBottomWidth)
    }
    const room = railEl.clientHeight - reserve
    fit = Math.max(1, Math.floor((room + gap) / (size + gap)))
  }

  $: total = $spaces.length
  $: slots = fit >= total ? total : Math.max(1, fit - 1)
  $: shown = $spaces.slice(0, slots)
  $: folded = $spaces.slice(slots)
  $: foldedActive = folded.some((s) => s.key === $space)
  $: foldedNews =
    !$focus.active &&
    folded.some(
      (s) =>
        (s.key === 'mail' && unread > 0) ||
        (s.key === 'feeds' && $feedsUnread > 0) ||
        (s.key === 'chat' && $chatUnreadRooms > 0) ||
        (s.key === 'tasks' && $taskBadge > 0),
    )

  function openMore(event: MouseEvent): void {
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
    const x = side === 'left' ? rect.right : rect.left
    openContextMenu(
      x,
      rect.top,
      folded.map((s) => ({ label: $t(s.labelKey), hint: spaceKeys(s.key) ?? '', action: () => openSpace(s.key) })),
    )
  }

  // --- the marker -----------------------------------------------------------------
  // the marker follows the active entry where it actually sits, so it stays
  // right while entries are dragged, moved or folded.
  let markerY: number | null = null
  let markerReady = false

  function placeMarker(): void {
    const el = groupEl?.querySelector<HTMLElement>('.entry.active')
    markerY = el ? el.offsetTop : null
    if (markerY !== null && !markerReady) {
      requestAnimationFrame(() => (markerReady = true))
    }
  }

  // runs after the next update, whatever changed among its arguments.
  function after(fn: () => void, ..._deps: unknown[]): void {
    void tick().then(fn)
  }

  $: after(placeMarker, $space, shown, folded.length)

  let observer: MutationObserver | undefined
  let resize: ResizeObserver | undefined

  onMount(() => {
    observer = new MutationObserver(placeMarker)
    observer.observe(groupEl, { childList: true, subtree: true })
    if (typeof ResizeObserver === 'function') {
      resize = new ResizeObserver(() => {
        measure()
        void tick().then(placeMarker)
      })
      resize.observe(railEl)
      resize.observe(footEl)
    }
    measure()
  })

  onDestroy(() => {
    observer?.disconnect()
    resize?.disconnect()
  })

  $: if (railEl) {
    after(measure, pins.length, total)
  }

  // --- reordering -----------------------------------------------------------------
  let dragging = false

  function onReorder(event: CustomEvent<ReorderDetail>): void {
    reorderShownSpaces(event.detail.ids)
  }

  function onSpaceContext(event: MouseEvent, entry: NavEntry): void {
    openContextMenu(event.clientX, event.clientY, spaceMenu(entry, 'y'))
  }

  // alt with an arrow moves the focused entry, and focus goes with it.
  async function onSpaceKeydown(event: KeyboardEvent, entry: NavEntry): Promise<void> {
    if (!event.altKey || entry.transient || (event.key !== 'ArrowUp' && event.key !== 'ArrowDown')) {
      return
    }
    event.preventDefault()
    event.stopPropagation()
    moveSpaceShown(entry.key, event.key === 'ArrowUp' ? -1 : 1)
    await tick()
    groupEl.querySelector<HTMLElement>(`[data-space="${entry.key}"]`)?.focus()
  }

  // the number key of a space's place in the navigation, or its own key when
  // it has no number.
  $: spaceKeys = (key: Space): string | undefined => {
    const place = $visibleSpaces.findIndex((s) => s.key === key)
    return (place >= 0 && place < 9 ? keysFor(`go-space-${place + 1}`) : undefined) ?? keysFor(`go-${key}`)
  }

  function spaceTip(entry: NavEntry): string {
    const name = $t(entry.labelKey)
    return entry.transient ? `${name} (${$t('spaces.notPinned')})` : name
  }

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

  $: settingsLabel = $whatsNewUnseen ? `${$t('settings.title')} (${$t('whatsnew.unseen')})` : $t('settings.title')
</script>

<nav class="rail" class:right={side === 'right'} aria-label={$t('space.switcher')} style="--wails-draggable:drag" bind:this={railEl}>
  <div class="group spaces" bind:this={groupEl}>
    <span
      class="marker"
      class:ready={markerReady}
      class:gone={markerY === null}
      style={`--marker-y:${markerY ?? 0}px`}
      aria-hidden="true"
    ></span>
    <div
      class="list"
      class:dragging
      use:reorder
      on:reorder={onReorder}
      on:reorderstart={() => (dragging = true)}
      on:reorderend={() => (dragging = false)}
    >
    {#each shown as entry (entry.key)}
      <button
        type="button"
        class="entry"
        class:active={$space === entry.key}
        class:transient={entry.transient}
        data-space={entry.key}
        data-reorder-id={entry.transient ? undefined : entry.key}
        data-reorder-handle={entry.transient ? undefined : ''}
        aria-label={spaceTip(entry)}
        aria-current={$space === entry.key ? 'page' : undefined}
        use:tip={{ text: spaceTip(entry), shortcut: spaceKeys(entry.key), side: tipSide }}
        use:maildrop={spaceDrop(entry.key)}
        animate:flip={{ duration: dur(motion.base) }}
        on:click={() => openSpace(entry.key)}
        on:contextmenu|preventDefault|stopPropagation={(e) => onSpaceContext(e, entry)}
        on:keydown={(e) => onSpaceKeydown(e, entry)}
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
        {#if entry.key === 'chat' && $chatUnreadRooms > 0 && !$focus.active}
          {#if $chatHasHighlight}
            <span class="count" aria-hidden="true">{$chatUnreadRooms > 99 ? '99+' : $chatUnreadRooms}</span>
          {:else}
            <span class="badge" aria-hidden="true"></span>
          {/if}
        {/if}
      </button>
    {/each}
    </div>
    {#if folded.length > 0}
      <button
        type="button"
        class="entry more"
        class:active={foldedActive}
        aria-label={$t('spaces.more')}
        aria-haspopup="menu"
        use:tip={{ text: $t('spaces.more'), side: tipSide }}
        on:click={openMore}
      >
        <Ph svg={moreIcon} size={19} />
        {#if foldedNews}
          <span class="badge" aria-hidden="true"></span>
        {/if}
      </button>
    {/if}
  </div>

  {#if pins.length > 0}
    <div class="group pins" aria-label={$t('rail.pins')} bind:this={pinsEl}>
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

  <div class="group foot" bind:this={footEl}>
    <button
      type="button"
      class="entry"
      aria-label={$t('shortcut.search')}
      use:tip={{ text: $t('shortcut.search'), shortcut: keysFor('search'), side: tipSide }}
      on:click={() => dispatch('search')}
    >
      <Ph svg={searchIcon} size={19} />
    </button>
    {#if $profiles.length > 1}
      <button
        type="button"
        class="entry profile"
        aria-label={$t('profiles.current').replace('{name}', $currentProfile?.name ?? '')}
        use:tip={{ text: $t('profiles.current').replace('{name}', $currentProfile?.name ?? ''), shortcut: keysFor('next-profile'), side: tipSide }}
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
      aria-label={settingsLabel}
      use:tip={{ text: settingsLabel, shortcut: keysFor('preferences'), side: tipSide }}
      on:click={() => dispatch('settings')}
    >
      <Ph svg={settingsIcon} size={19} />
      {#if $whatsNewUnseen}
        <span class="badge" aria-hidden="true"></span>
      {/if}
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
    overflow: hidden;
  }

  .group {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    --wails-draggable: no-drag;
  }

  .spaces,
  .foot {
    flex-shrink: 0;
  }

  .list {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .list.dragging .entry {
    cursor: grabbing;
  }

  /* an open space the user left out of the navigation: there for now, drawn
     as a dashed outline rather than a solid entry. */
  .entry.transient {
    outline: var(--hairline) dashed var(--border-strong);
    outline-offset: calc(-1 * var(--space-1));
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
    transform: translateY(var(--marker-y));
  }

  .marker.ready {
    transition: transform var(--motion-base) var(--ease-out);
  }

  .marker.gone {
    visibility: hidden;
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
