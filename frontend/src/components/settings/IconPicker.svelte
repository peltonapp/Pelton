<script lang="ts">
  // IconPicker lets the user choose an icon: a custom menu entry or a project.
  // Icons from the active theme are offered first (they restyle live with the
  // theme) unless themeIcons is off, then the full Tabler set, searchable by
  // name. The Tabler geometry is a ~2MB dataset, so it is lazy-imported only
  // when the picker mounts, keeping it out of the main bundle; the chosen
  // tabler icon's node geometry is returned so the always-on UI can render it
  // without that dataset. Arrow keys move through the results from the search
  // field and Enter picks the highlighted one.
  import { createEventDispatcher, onMount, tick } from 'svelte'
  import { IconSearch, IconX } from '@tabler/icons-svelte'
  import MenuGlyph from '../common/MenuGlyph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import { iconOverrides } from '../../theme/icons'
  import { t } from '../../lib/i18n'
  import type { IconNode } from '../../lib/menuactions'

  /** Offer the active theme's icons before the Tabler set. */
  export let themeIcons = true
  /** Label of the button that clears the choice, "No icon" when empty. */
  export let clearLabel = ''

  const dispatch = createEventDispatcher<{
    select: { iconName: string; iconNodes?: IconNode[] }
    clear: void
  }>()

  // how many tabler matches to render at once; more than this and the user is
  // asked to narrow the search, so the grid never mounts thousands of svgs.
  const maxResults = 120

  let nodes: Record<string, IconNode[]> | null = null
  let query = ''
  let active = 0
  let input: HTMLInputElement
  let results: HTMLDivElement

  onMount(async () => {
    // a hosting popover moves its content to the end of <body> and reveals it
    // a frame later, which would drop an earlier focus, so focus again then.
    input?.focus()
    requestAnimationFrame(() => input?.focus())
    nodes = (await import('tabler-nodes-outline')).default
  })

  // normalize strips separators so "mail open" matches "mail-opened".
  function normalize(value: string): string {
    return value.toLowerCase().replace(/[\s-]/g, '')
  }

  $: q = normalize(query)
  $: themeNames = themeIcons
    ? Object.keys($iconOverrides)
        .filter((name) => q === '' || normalize(name).includes(q))
        .sort()
    : []
  $: allTabler = nodes ? Object.keys(nodes) : []
  $: tablerMatches = q === '' ? allTabler : allTabler.filter((name) => normalize(name).includes(q))
  $: tablerShown = tablerMatches.slice(0, maxResults)
  $: total = themeNames.length + tablerShown.length
  $: if (q !== undefined) {
    active = 0
  }
  $: if (active >= total) {
    active = Math.max(0, total - 1)
  }

  function pickTheme(name: string): void {
    dispatch('select', { iconName: name })
  }

  function pickTabler(name: string): void {
    dispatch('select', { iconName: name, iconNodes: nodes?.[name] })
  }

  function pickActive(): void {
    if (active < themeNames.length) {
      pickTheme(themeNames[active])
    } else if (tablerShown[active - themeNames.length]) {
      pickTabler(tablerShown[active - themeNames.length])
    }
  }

  // columns reads how many cells a row of the grid holds, 1 when the browser
  // has not resolved the template to pixel tracks.
  function columns(): number {
    const grid = results?.querySelector('.grid')
    if (!grid) {
      return 1
    }
    const tracks = getComputedStyle(grid).gridTemplateColumns.split(' ').filter(Boolean)
    return tracks.length > 0 && tracks.every((track) => track.endsWith('px')) ? tracks.length : 1
  }

  async function move(delta: number): Promise<void> {
    if (total === 0) {
      return
    }
    active = Math.min(Math.max(active + delta, 0), total - 1)
    await tick()
    results?.querySelector('[aria-selected="true"]')?.scrollIntoView?.({ block: 'nearest' })
  }

  function onKeydown(event: KeyboardEvent): void {
    const rtl = getComputedStyle(document.documentElement).direction === 'rtl'
    const atStart = input.selectionStart === 0 && input.selectionEnd === 0
    const atEnd = input.selectionStart === query.length && input.selectionEnd === query.length
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault()
        void move(columns())
        break
      case 'ArrowUp':
        event.preventDefault()
        void move(-columns())
        break
      case 'ArrowRight':
        if (rtl ? atStart : atEnd) {
          event.preventDefault()
          void move(rtl ? -1 : 1)
        }
        break
      case 'ArrowLeft':
        if (rtl ? atEnd : atStart) {
          event.preventDefault()
          void move(rtl ? 1 : -1)
        }
        break
      case 'Enter':
        event.preventDefault()
        event.stopPropagation()
        pickActive()
        break
    }
  }
</script>

<div class="picker">
  <div class="search">
    <IconSearch size={15} stroke={1.7} />
    <input
      bind:this={input}
      type="text"
      role="combobox"
      aria-expanded="true"
      aria-controls="icon-picker-results"
      aria-activedescendant={total > 0 ? `icon-picker-${active}` : undefined}
      bind:value={query}
      placeholder={$t('menuBar.icon.search')}
      aria-label={$t('menuBar.icon.search')}
      on:keydown={onKeydown}
    />
    <button type="button" class="clear" on:click={() => dispatch('clear')}>
      <IconX size={14} stroke={1.7} />
      <span>{clearLabel || $t('menuBar.icon.none')}</span>
    </button>
  </div>

  <div class="scroll" bind:this={results} id="icon-picker-results" role="listbox" aria-label={$t('menuBar.icon.tablerGroup')}>
    {#if themeNames.length > 0}
      <div class="group-label">{$t('menuBar.icon.themeGroup')}</div>
      <div class="grid">
        {#each themeNames as name, i (name)}
          <button
            type="button"
            class="cell"
            class:active={active === i}
            id={`icon-picker-${i}`}
            role="option"
            aria-selected={active === i}
            tabindex="-1"
            title={name}
            on:click={() => pickTheme(name)}
          >
            <MenuGlyph iconName={name} size={20} />
          </button>
        {/each}
      </div>
    {/if}

    {#if themeIcons}
      <div class="group-label">{$t('menuBar.icon.tablerGroup')}</div>
    {/if}
    {#if nodes === null}
      <div class="hint loading"><PieProgress size={16} label={$t('menuBar.icon.loading')} /><span>{$t('menuBar.icon.loading')}</span></div>
    {:else}
      <div class="grid">
        {#each tablerShown as name, i (name)}
          {@const index = themeNames.length + i}
          <button
            type="button"
            class="cell"
            class:active={active === index}
            id={`icon-picker-${index}`}
            role="option"
            aria-selected={active === index}
            tabindex="-1"
            title={name}
            on:click={() => pickTabler(name)}
          >
            <MenuGlyph iconNodes={nodes[name]} size={20} />
          </button>
        {/each}
      </div>
      {#if tablerMatches.length === 0}
        <div class="hint">{$t('menuBar.icon.noMatches')}</div>
      {:else if tablerMatches.length > tablerShown.length}
        <div class="hint">{$t('menuBar.icon.narrow')}</div>
      {/if}
    {/if}
  </div>
</div>

<style>
  .picker {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .search {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-tertiary);
  }

  .search input {
    flex: 1;
    min-width: 0;
    padding: var(--space-2) 0;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font-size: var(--fz-label);
    outline: none;
  }

  .clear {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-1) var(--space-2);
    border: none;
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .clear:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .scroll {
    max-height: 240px;
    overflow-y: auto;
  }

  .group-label {
    padding: var(--space-2) var(--space-1) var(--space-1);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(34px, 1fr));
    gap: var(--space-1);
  }

  .cell {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    aspect-ratio: 1;
    border: var(--hairline) solid transparent;
    background: transparent;
    color: var(--text-secondary);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .cell:hover,
  .cell.active {
    background: var(--surface-hover);
    color: var(--text-primary);
    border-color: var(--border-subtle);
  }

  .cell.active {
    border-color: var(--accent);
  }

  .hint {
    padding: var(--space-2) var(--space-1);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .loading {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
</style>
