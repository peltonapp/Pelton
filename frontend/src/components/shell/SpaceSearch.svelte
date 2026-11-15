<script lang="ts">
  // the top bar search of a space other than mail. the space hands in what to
  // search and what opening a hit does; this is the field and its dropdown.
  import { onDestroy, onMount, tick } from 'svelte'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { t } from '../../lib/i18n'
  import { prefs } from '../../stores/prefs'
  import { menuHint, bindings } from '../../stores/shortcuts'
  import type { SpaceSearchProvider, SpaceSearchResult } from '../../lib/spacesearch'

  /** What this field searches and how a hit opens. */
  export let provider: SpaceSearchProvider

  const listId = 'space-search-list'
  const debounceMs = 110

  let wrap: HTMLDivElement
  let inputEl: HTMLInputElement
  let text = ''
  let results: SpaceSearchResult[] = []
  let searched = ''
  let open = false
  let activeIndex = 0
  let optionEls: HTMLElement[] = []
  let timer: ReturnType<typeof setTimeout> | undefined
  let run = 0
  let bandTop = 0
  let bandHeight = 0

  $: searchKeys = $bindings.search ? menuHint('search') : ''
  $: query = text.trim()
  $: showList = open && query !== '' && searched === query
  $: placeBand(activeIndex, results, showList)

  // a new provider is another space: start empty.
  $: reset(provider)

  function reset(_provider: SpaceSearchProvider): void {
    clearTimeout(timer)
    run += 1
    text = ''
    results = []
    searched = ''
    open = false
  }

  onMount(() => document.addEventListener('pointerdown', onOutside, true))
  onDestroy(() => {
    clearTimeout(timer)
    document.removeEventListener('pointerdown', onOutside, true)
  })

  function onOutside(event: PointerEvent): void {
    if (wrap && !wrap.contains(event.target as Node)) {
      open = false
    }
  }

  function onInput(event: Event): void {
    text = (event.currentTarget as HTMLInputElement).value
    open = true
    clearTimeout(timer)
    const wanted = text.trim()
    if (wanted === '') {
      results = []
      searched = ''
      return
    }
    timer = setTimeout(() => void search(wanted), debounceMs)
  }

  async function search(wanted: string): Promise<void> {
    const mine = ++run
    let found: SpaceSearchResult[] = []
    try {
      found = await provider.search(wanted)
    } catch {
      found = []
    }
    // a slower, older search must not replace a newer one's hits.
    if (mine !== run) {
      return
    }
    results = found
    searched = wanted
    activeIndex = 0
  }

  async function placeBand(index: number, _results: SpaceSearchResult[], shown: boolean): Promise<void> {
    if (!shown) {
      return
    }
    await tick()
    const el = optionEls[index]
    if (el) {
      bandTop = el.offsetTop
      bandHeight = el.offsetHeight
      el.scrollIntoView({ block: 'nearest' })
    }
  }

  function choose(result: SpaceSearchResult): void {
    open = false
    inputEl?.blur()
    provider.open(result)
  }

  function clear(): void {
    clearTimeout(timer)
    run += 1
    text = ''
    results = []
    searched = ''
    inputEl?.focus()
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      if (results.length === 0) {
        return
      }
      event.preventDefault()
      open = true
      const step = event.key === 'ArrowDown' ? 1 : -1
      activeIndex = (activeIndex + step + results.length) % results.length
    } else if (event.key === 'Enter') {
      const hit = showList ? results[activeIndex] : undefined
      if (hit) {
        event.preventDefault()
        choose(hit)
      }
    } else if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      if (open && query !== '') {
        open = false
      } else if (text !== '') {
        clear()
      } else {
        inputEl?.blur()
      }
    }
  }
</script>

<div class="wrap" bind:this={wrap}>
  <div class="field">
    <span class="lead"><Ph svg={searchIcon} size={15} /></span>
    <input
      bind:this={inputEl}
      data-top-search
      data-space-search
      type="text"
      role="combobox"
      autocomplete="off"
      spellcheck="false"
      aria-label={$t(provider.placeholderKey)}
      aria-expanded={showList}
      aria-controls={listId}
      aria-autocomplete="list"
      aria-activedescendant={showList && results.length > 0 ? `${listId}-${activeIndex}` : undefined}
      placeholder={$t(provider.placeholderKey)}
      value={text}
      on:input={onInput}
      on:focus={() => (open = true)}
      on:keydown={onKeydown}
    />
    {#if text !== ''}
      <button type="button" class="clear" aria-label={$t('messageList.search.clearSearch')} on:click={clear}>
        <Ph svg={xIcon} size={13} />
      </button>
    {:else if searchKeys && $prefs.showShortcutHints}
      <kbd>{searchKeys}</kbd>
    {/if}
  </div>

  {#if showList}
    <div class="list" id={listId} role="listbox" aria-label={$t(provider.placeholderKey)}>
      {#if results.length === 0}
        <p class="empty">{$t(provider.emptyKey)}</p>
      {:else}
        <span class="band" style={`transform: translateY(${bandTop}px); height: ${bandHeight}px`} aria-hidden="true"></span>
        {#each results as result, i (result.id)}
          <!-- svelte-ignore a11y-click-events-have-key-events -->
          <div
            class="option"
            id={`${listId}-${i}`}
            role="option"
            aria-selected={i === activeIndex}
            tabindex="-1"
            bind:this={optionEls[i]}
            on:pointerenter={() => (activeIndex = i)}
            on:mousedown={(e) => e.preventDefault()}
            on:click={() => choose(result)}
          >
            {#if result.color}
              <span class="dot" style={`background:${result.color}`} aria-hidden="true"></span>
            {/if}
            <span class="text">
              <span class="title">{result.title}</span>
              {#if result.detail}
                <span class="detail">{result.detail}</span>
              {/if}
            </span>
            {#if result.aside}
              <span class="aside">{result.aside}</span>
            {/if}
          </div>
        {/each}
      {/if}
    </div>
  {/if}
</div>

<style>
  .wrap {
    position: relative;
    width: 100%;
    --wails-draggable: no-drag;
  }

  .field {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    height: calc(var(--control-height) - var(--space-2));
    padding-inline: var(--space-3) var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-tertiary);
    cursor: text;
    transition:
      border-color var(--motion-fast) var(--ease-out),
      background-color var(--motion-fast) var(--ease-out);
  }

  .field:hover {
    border-color: var(--border-strong);
  }

  .field:focus-within {
    border-color: var(--accent);
    color: var(--text-secondary);
  }

  .lead {
    display: inline-flex;
  }

  input {
    flex: 1;
    min-width: 0;
    height: 100%;
    padding: 0;
    border: none;
    outline: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
  }

  input::placeholder {
    color: var(--text-tertiary);
  }

  .clear {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-1);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition:
      color var(--motion-fast) var(--ease-out),
      background-color var(--motion-fast) var(--ease-out);
  }

  .clear:hover {
    color: var(--text-primary);
    background: var(--surface-hover);
  }

  kbd {
    flex-shrink: 0;
    padding-inline-end: var(--space-2);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
  }

  .list {
    position: absolute;
    top: calc(100% + var(--space-2));
    inset-inline: 0;
    z-index: 60;
    max-height: var(--suggest-max-height);
    overflow-y: auto;
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    animation: rise var(--motion-base) var(--ease-out);
  }

  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(calc(var(--space-2) * -1));
    }
  }

  .band {
    position: absolute;
    top: 0;
    inset-inline: var(--space-1);
    border-radius: var(--radius-control);
    background: var(--surface-hover);
    pointer-events: none;
    transition:
      transform var(--motion-fast) var(--ease-out),
      height var(--motion-fast) var(--ease-out);
  }

  .option {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: calc(var(--control-height) - var(--space-2));
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-control);
    color: var(--text-primary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    outline: none;
  }

  .dot {
    flex-shrink: 0;
    width: 8px;
    height: 8px;
    border-radius: 50%;
  }

  .text {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }

  .title,
  .detail {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .detail,
  .aside {
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .aside {
    flex-shrink: 0;
    max-width: 45%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .empty {
    margin: 0;
    padding: var(--space-3);
    color: var(--text-tertiary);
    font-size: var(--fz-label);
  }
</style>
