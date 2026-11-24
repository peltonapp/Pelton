<script lang="ts">
  // a themeable replacement for <select> (#401).
  //
  // A native select draws its popup with operating system chrome that no theme
  // can reach, so a dark or custom Pelton theme still opened a grey Windows or
  // macOS list. This renders the list itself, which means re-implementing what
  // the browser was doing for free: keyboard navigation, typeahead, focus
  // handling and the aria roles a screen reader needs. The navigation rules
  // live in lib/selectnav.ts and are tested there.
  //
  // The popup is positioned fixed rather than absolute. Most of these sit in
  // the settings panel, which scrolls; an absolutely positioned list would be
  // clipped by that scroll container as soon as a row near the bottom was
  // opened. It is also moved to the end of <body>, because a fixed position is
  // only measured against the viewport while no ancestor carries a transform:
  // see lib/portal.ts.
  import { createEventDispatcher, onDestroy, tick } from 'svelte'
  import { IconChevronDown, IconCheck } from '@tabler/icons-svelte'
  import { currentUIScale } from '../../theme/theme'
  import { placeBelow } from '../../lib/popupplace'
  import { portal } from '../../lib/portal'
  import { popPopup, pushPopup } from '../../lib/popups'
  import {
    flatten,
    indexOfValue,
    isGroup,
    step,
    firstEnabled,
    typeahead,
    typeaheadResetMs,
    type SelectItem,
  } from '../../lib/selectnav'

  /** The chosen value. Two-way bindable. */
  export let value: string
  /** Options, groups of options, or a mix. */
  export let items: SelectItem[] = []
  export let disabled = false
  /** Accessible name, for a select with no visible <label> of its own. */
  export let ariaLabel: string | undefined = undefined
  /** Id for the button, so a <label for> can name it. A button is labelable, so
   * that association survives the move away from a native select. */
  export let id: string | undefined = undefined
  /** Extra classes on the button, so callers can size it as they did the select. */
  let extraClass = ''
  export { extraClass as class }

  const dispatch = createEventDispatcher<{ change: string }>()

  let open = false
  let buttonEl: HTMLButtonElement
  let listEl: HTMLUListElement | undefined
  let active = -1
  let buffer = ''
  let bufferTimer: ReturnType<typeof setTimeout> | undefined
  let box = { top: 0, start: 0, width: 0, below: true }

  $: options = flatten(items)

  // one flat list to render: group labels and options interleaved, each option
  // carrying its index in `options` so the markup never has to look it up. A
  // listbox is allowed to hold presentational headings, and flattening keeps a
  // single branch for an option rather than one per nesting level.
  $: rows = items.flatMap((item) =>
    isGroup(item)
      ? [
          { group: item.label, option: null, index: -1 },
          ...item.options.map((option) => ({ group: null, option, index: options.indexOf(option) })),
        ]
      : [{ group: null, option: item, index: options.indexOf(item) }],
  )

  $: selected = options[indexOfValue(options, value)]
  // a value with no matching option shows as empty rather than as the first
  // option, so a stale setting reads as unset instead of silently looking like
  // a choice nobody made.
  $: label = selected?.label ?? ''

  const listId = `select-list-${Math.random().toString(36).slice(2, 9)}`

  // place positions the popup against the button. The arithmetic, including
  // the conversion out of the interface zoom, is in lib/popupplace.ts.
  // inset-inline-start resolves against the document's own direction, which is
  // what `start` is measured against.
  function place(): void {
    const rect = buttonEl.getBoundingClientRect()
    box = placeBelow(
      rect,
      { width: window.innerWidth, height: window.innerHeight },
      currentUIScale(),
      document.documentElement.dir === 'rtl',
    )
  }

  async function openList(startAt = indexOfValue(options, value)): Promise<void> {
    if (disabled || open) {
      return
    }
    place()
    active = startAt >= 0 ? startAt : firstEnabled(options)
    open = true
    pushPopup()
    await tick()
    // focused here rather than by an action on the list, so it happens after
    // the portal has moved the list: focus does not survive being reparented.
    listEl?.focus()
    scrollActiveIntoView()
  }

  function closeList(refocus = true): void {
    if (!open) {
      return
    }
    open = false
    popPopup()
    buffer = ''
    if (refocus) {
      buttonEl?.focus()
    }
  }

  // a picker can be unmounted with its list still open, by the dialog around it
  // closing. the count has to come back down or escape stops reaching dialogs.
  onDestroy(() => {
    if (open) {
      popPopup()
    }
  })

  function choose(index: number): void {
    const option = options[index]
    if (!option || option.disabled) {
      return
    }
    if (option.value !== value) {
      value = option.value
      dispatch('change', option.value)
    }
    closeList()
  }

  function scrollActiveIntoView(): void {
    listEl?.querySelector<HTMLElement>('[data-active="true"]')?.scrollIntoView({ block: 'nearest' })
  }

  async function moveTo(index: number): Promise<void> {
    if (index < 0) {
      return
    }
    active = index
    await tick()
    scrollActiveIntoView()
  }

  function onButtonKeydown(event: KeyboardEvent): void {
    if (disabled) {
      return
    }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp' || event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      void openList()
    }
  }

  function onListKeydown(event: KeyboardEvent): void {
    switch (event.key) {
      case 'Escape':
        event.preventDefault()
        closeList()
        return
      case 'Tab':
        // Tab leaves the control, as it does on a native select. The highlight
        // is abandoned rather than chosen: only Enter commits. Focus goes back
        // to the button first so the browser walks on from there, and so a
        // dialog's focus trap still sees focus inside itself: the list is not
        // in the dialog's subtree any more.
        closeList()
        return
      case 'Enter':
      case ' ':
        event.preventDefault()
        choose(active)
        return
      case 'ArrowDown':
        event.preventDefault()
        void moveTo(step(options, active, 1))
        return
      case 'ArrowUp':
        event.preventDefault()
        void moveTo(step(options, active, -1))
        return
      case 'Home':
        event.preventDefault()
        void moveTo(firstEnabled(options))
        return
      case 'End':
        event.preventDefault()
        void moveTo(firstEnabled(options, false))
        return
    }
    // anything else printable feeds the typeahead.
    if (event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey) {
      event.preventDefault()
      typeaheadKey(event.key)
    }
  }

  function typeaheadKey(key: string): void {
    clearTimeout(bufferTimer)
    buffer += key
    bufferTimer = setTimeout(() => (buffer = ''), typeaheadResetMs)
    void moveTo(typeahead(options, buffer, active))
  }
</script>

<svelte:window
  on:resize={() => open && place()}
  on:scroll|capture={() => open && place()}
/>

<button
  type="button"
  bind:this={buttonEl}
  {id}
  class="select {extraClass}"
  class:open
  {disabled}
  role="combobox"
  aria-haspopup="listbox"
  aria-expanded={open}
  aria-controls={open ? listId : undefined}
  aria-label={ariaLabel}
  on:click={() => (open ? closeList() : openList())}
  on:keydown={onButtonKeydown}
>
  {#if selected?.color}<span class="dot" style:--dot={selected.color} aria-hidden="true"></span>{/if}
  <span class="label">{label}</span>
  <IconChevronDown size={14} stroke={1.8} class="chevron" />
</button>

{#if open}
  <div class="popup" use:portal>
    <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
    <div class="scrim" on:click={() => closeList()}></div>

    <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
    <ul
      bind:this={listEl}
      id={listId}
      class="list"
      class:above={!box.below}
      role="listbox"
      aria-label={ariaLabel}
      tabindex="-1"
      style="top: {box.top}px; inset-inline-start: {box.start}px; min-width: {box.width}px;"
      on:keydown={onListKeydown}
    >
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- the keys are handled once on the listbox above, which is what the aria
           listbox pattern asks for: options are not individually focusable, they
           are pointed at by the active highlight. -->
      {#each rows as row, i (row.group !== null ? `g${i}` : row.option.value)}
        {#if row.group !== null}
          <li class="group-label" role="presentation">{row.group}</li>
        {:else}
          <li
            role="option"
            aria-selected={row.option.value === value}
            aria-disabled={row.option.disabled || undefined}
            class="opt"
            class:active={row.index === active}
            data-active={row.index === active}
            on:click={() => choose(row.index)}
            on:mousemove={() => (active = row.index)}
          >
            <span class="tick">
              {#if row.option.value === value}<IconCheck size={13} stroke={2} />{/if}
            </span>
            {#if row.option.color}<span class="dot" style:--dot={row.option.color} aria-hidden="true"></span>{/if}
            <span class="opt-label">{row.option.label}</span>
          </li>
        {/if}
      {/each}
    </ul>
  </div>
{/if}

<style>
  .select {
    display: inline-flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
  }

  .select:disabled {
    cursor: default;
    opacity: 0.55;
  }

  .select:not(:disabled):hover,
  .select.open {
    border-color: var(--accent);
  }

  .select:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .dot {
    flex-shrink: 0;
    width: var(--space-3);
    height: var(--space-3);
    border-radius: 50%;
    background: var(--dot);
  }

  .label {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* 320 is the popup band Modal reserves above the overlays it stacks from
     300, so a picker opened inside a dialog draws over it rather than under. */
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 320;
  }

  .list {
    position: fixed;
    z-index: 321;
    max-height: 280px;
    overflow-y: auto;
    margin: 0;
    padding: var(--space-1);
    list-style: none;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
  }

  /* flipped above the button: the fixed top is the button's own top edge, so
     the list has to be pulled up by its own height. */
  .list.above {
    transform: translateY(-100%);
    margin-top: -4px;
  }

  .group-label {
    display: block;
    padding: var(--space-2) var(--space-2) var(--space-1);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .opt {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2);
    border-radius: var(--radius-control);
    color: var(--text-primary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .opt.active {
    background: var(--surface-hover);
  }

  .opt[aria-disabled='true'] {
    color: var(--text-tertiary);
    cursor: default;
  }

  /* the tick keeps its column whether or not it is showing, so labels do not
     shift sideways as the choice moves. */
  .tick {
    display: inline-flex;
    justify-content: center;
    width: 13px;
    flex-shrink: 0;
    color: var(--accent);
  }

  .opt-label {
    flex: 1;
    white-space: nowrap;
  }
</style>
