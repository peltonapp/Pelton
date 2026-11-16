<script lang="ts">
  // the quick switcher of the notes space: type part of a note's name, alias
  // or folder and open it, or make a note of that name when none matches. the
  // same list picks a template for "new from template".
  import { createEventDispatcher, onDestroy, onMount, tick } from 'svelte'
  import { fade } from 'svelte/transition'
  import { fuzzyMatch } from '../../lib/fuzzy'
  import { noteFolder } from '../../lib/notemd'
  import { popPopup, pushPopup } from '../../lib/popups'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { VaultNote } from '../../lib/types'

  /** The notes to choose from. */
  export let notes: VaultNote[]
  /** The notes opened last, ranked first while nothing is typed. */
  export let recent: string[] = []
  /** Offers to create a note named after the query. */
  export let allowCreate = true
  /** The field's placeholder. */
  export let placeholder = ''
  /** Accessible name of the dialog. */
  export let label = ''

  const dispatch = createEventDispatcher<{ pick: { path: string; newTab: boolean }; create: string; close: void }>()
  const limit = 60

  let query = ''
  let selected = 0
  let input: HTMLInputElement
  let listEl: HTMLUListElement

  interface Row {
    path: string
    title: string
    detail: string
    create?: boolean
  }

  $: rows = rank(notes, query, recent)
  $: if (selected >= rows.length) {
    selected = Math.max(0, rows.length - 1)
  }

  function rank(list: VaultNote[], q: string, rec: string[]): Row[] {
    const text = q.trim()
    if (!text) {
      const order = new Map(rec.map((p, i) => [p, i]))
      return [...list]
        .sort((a, b) => (order.get(a.path) ?? 1e9) - (order.get(b.path) ?? 1e9) || b.mtime - a.mtime)
        .slice(0, limit)
        .map((n) => ({ path: n.path, title: n.title, detail: noteFolder(n.path) }))
    }
    const scored: { row: Row; score: number }[] = []
    for (const n of list) {
      let best = fuzzyMatch(text, n.title)?.score ?? -Infinity
      let alias = ''
      for (const a of n.aliases) {
        const s = fuzzyMatch(text, a)?.score ?? -Infinity
        if (s > best) {
          best = s
          alias = a
        }
      }
      const pathScore = (fuzzyMatch(text, n.path)?.score ?? -Infinity) - 1
      if (pathScore > best) {
        best = pathScore
        alias = ''
      }
      if (best > -Infinity) {
        scored.push({ row: { path: n.path, title: alias ? `${alias}` : n.title, detail: alias ? `${n.title}${noteFolder(n.path) ? `, ${noteFolder(n.path)}` : ''}` : noteFolder(n.path) }, score: best })
      }
    }
    scored.sort((a, b) => b.score - a.score || a.row.path.localeCompare(b.row.path))
    const out = scored.slice(0, limit).map((s) => s.row)
    if (allowCreate && !list.some((n) => n.title.toLowerCase() === text.toLowerCase())) {
      out.push({ path: '', title: text, detail: $t('notes.switcher.create'), create: true })
    }
    return out
  }

  function choose(i: number, newTab: boolean): void {
    const row = rows[i]
    if (!row) {
      return
    }
    if (row.create) {
      dispatch('create', row.title)
    } else {
      dispatch('pick', { path: row.path, newTab })
    }
  }

  async function onKeydown(e: KeyboardEvent): Promise<void> {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      const n = rows.length
      if (n) {
        selected = (selected + (e.key === 'ArrowDown' ? 1 : -1) + n) % n
        await tick()
        listEl?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: 'nearest' })
      }
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (e.shiftKey && allowCreate && query.trim()) {
        dispatch('create', query.trim())
        return
      }
      choose(selected, e.metaKey || e.ctrlKey)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      dispatch('close')
    }
  }

  onMount(() => {
    pushPopup()
    input?.focus()
  })
  onDestroy(popPopup)
</script>

<!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
<div class="scrim" transition:fade={{ duration: dur(motion.fast) }} on:click={() => dispatch('close')}></div>
<div class="switcher" role="dialog" aria-modal="true" aria-label={label} transition:fade={{ duration: dur(motion.fast) }}>
  <input
    bind:this={input}
    bind:value={query}
    class="field"
    type="text"
    {placeholder}
    aria-label={placeholder}
    aria-controls="note-switcher-list"
    on:input={() => (selected = 0)}
    on:keydown={(e) => void onKeydown(e)}
  />
  <ul id="note-switcher-list" class="rows" role="listbox" bind:this={listEl}>
    {#each rows as row, i (row.create ? ':create' : row.path)}
      <li
        role="option"
        aria-selected={i === selected}
        class="row"
        class:create={row.create}
        on:mousemove={() => (selected = i)}
        on:mousedown|preventDefault={(e) => choose(i, e.metaKey || e.ctrlKey)}
      >
        <span class="title">{row.title}</span>
        {#if row.detail}<span class="detail">{row.detail}</span>{/if}
      </li>
    {/each}
    {#if rows.length === 0}
      <li class="empty">{$t('notes.switcher.none')}</li>
    {/if}
  </ul>
  <footer class="hints">
    <span>{$t('notes.switcher.hintOpen')}</span>
    <span>{$t('notes.switcher.hintTab')}</span>
    {#if allowCreate}<span>{$t('notes.switcher.hintCreate')}</span>{/if}
  </footer>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 300;
    background: var(--scrim);
  }

  .switcher {
    position: fixed;
    z-index: 301;
    inset-block-start: 14%;
    inset-inline-start: 50%;
    transform: translateX(-50%);
    width: min(calc(var(--popover-width) * 1.8), calc(100% - 2 * var(--space-6)));
    display: flex;
    flex-direction: column;
    max-height: 70%;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    overflow: hidden;
    font-size: var(--fz-list);
  }

  :global([dir='rtl']) .switcher {
    transform: translateX(50%);
  }

  .field {
    height: calc(var(--control-height) + var(--space-3));
    padding: 0 var(--space-4);
    border: none;
    border-bottom: var(--hairline) solid var(--border-default);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
    outline: none;
  }

  .rows {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    list-style: none;
    margin: 0;
    padding: var(--space-1);
  }

  .row {
    display: flex;
    align-items: baseline;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .row[aria-selected='true'] {
    background: var(--selection-bg);
  }

  .title {
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .row.create .title {
    font-weight: var(--fw-medium);
  }

  .detail {
    margin-inline-start: auto;
    flex-shrink: 0;
    max-width: 50%;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .empty {
    padding: var(--space-4);
    color: var(--text-tertiary);
  }

  .hints {
    display: flex;
    gap: var(--space-4);
    padding: var(--space-2) var(--space-4);
    border-top: var(--hairline) solid var(--border-subtle);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
