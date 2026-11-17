<script lang="ts">
  // the right panel of the notes space, about the open note: the notes that
  // link to it (and those that name it without a link, with a button to make
  // the link), its outline, the links it makes, and its neighbourhood in the
  // graph.
  import { createEventDispatcher } from 'svelte'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import Select from '../common/Select.svelte'
  import NoteGraph from './NoteGraph.svelte'
  import { vaultBacklinks, vaultLinkMention, vaultUnlinkedMentions } from '../../lib/api'
  import { localGraph } from '../../lib/notegraph'
  import { noteTitle } from '../../lib/notemd'
  import { parsePeltonUrl } from '../../lib/peltonlinks'
  import { notesByPath, vaultNotes } from '../../stores/notes'
  import { errorMessage, toastError } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { VaultMention } from '../../lib/types'

  /** The vault path of the open note. */
  export let path: string
  /** The open note's headings, from the text being edited. */
  export let headings: { level: number; text: string; line: number }[] = []
  /** Which part shows. */
  export let tab: 'backlinks' | 'outline' | 'links' | 'graph' = 'backlinks'
  /** Bumped when the index changed, so the lists load again. */
  export let revision = 0

  const dispatch = createEventDispatcher<{
    open: { path: string; line?: number; newTab: boolean }
    line: number
    follow: { kind: 'wiki' | 'url'; target: string }
    tab: 'backlinks' | 'outline' | 'links' | 'graph'
    close: void
  }>()

  let linked: VaultMention[] = []
  let unlinked: VaultMention[] = []
  let loading = false
  let showUnlinked = false
  let depth = 1
  let graphTags = false

  $: note = $notesByPath.get(path)
  $: void loadMentions(path, revision, tab)
  $: groups = group(linked)
  $: unlinkedGroups = group(unlinked)
  $: links = note?.links ?? []
  $: noteLinks = dedupe(links.filter((l) => (l.kind === 'wiki' || l.kind === 'markdown') && !l.embed))
  $: objectLinks = dedupe(links.filter((l) => l.kind === 'pelton'))
  $: webLinks = dedupe(links.filter((l) => l.kind === 'external' && /^https?:/i.test(l.target)))
  $: graph = tab === 'graph' ? localGraph($vaultNotes, path, depth, { tags: graphTags, objects: true }) : { nodes: [], edges: [] }
  $: minLevel = headings.reduce((m, h) => Math.min(m, h.level), 6)

  function dedupe<T extends { target: string; subpath: string }>(list: T[]): T[] {
    const seen = new Set<string>()
    return list.filter((l) => {
      const key = `${l.target.toLowerCase()}#${l.subpath}`
      if (seen.has(key)) {
        return false
      }
      seen.add(key)
      return true
    })
  }

  function group(list: VaultMention[]): { source: string; lines: VaultMention[] }[] {
    const map = new Map<string, VaultMention[]>()
    for (const m of list) {
      const arr = map.get(m.source)
      if (arr) {
        arr.push(m)
      } else {
        map.set(m.source, [m])
      }
    }
    return [...map.entries()].map(([source, lines]) => ({ source, lines }))
  }

  let token = 0
  async function loadMentions(p: string, _rev: number, which: string): Promise<void> {
    if (which !== 'backlinks' || !p) {
      return
    }
    const mine = ++token
    loading = true
    try {
      const [a, b] = await Promise.all([vaultBacklinks(p), vaultUnlinkedMentions(p).catch(() => [])])
      if (mine === token) {
        linked = a
        unlinked = b
      }
    } catch {
      if (mine === token) {
        linked = []
        unlinked = []
      }
    } finally {
      if (mine === token) {
        loading = false
      }
    }
  }

  async function link(m: VaultMention): Promise<void> {
    try {
      await vaultLinkMention(m.source, m.line, m.match, path)
      unlinked = unlinked.filter((x) => x !== m)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function objectLabel(target: string, display: string): string {
    const p = parsePeltonUrl(target)
    const kind = p ? $t(`notes.object.${p.kind}`) : ''
    return `${kind}: ${display || p?.id || target}`
  }

  const tabs: ('backlinks' | 'outline' | 'links' | 'graph')[] = ['backlinks', 'outline', 'links', 'graph']
  const depthItems = ['1', '2', '3'].map((d) => ({ value: d, label: d }))
</script>

<aside class="panel" aria-label={$t('notes.panel')}>
  <header class="head">
    <div class="tabs" role="tablist">
      {#each tabs as key (key)}
        <button type="button" role="tab" class="tab" aria-selected={tab === key} on:click={() => dispatch('tab', key)}>{$t(`notes.panel.${key}`)}</button>
      {/each}
    </div>
    <button type="button" class="cal-icon" aria-label={$t('notes.panel.close')} on:click={() => dispatch('close')}>
      <Ph svg={xIcon} size={14} />
    </button>
  </header>

  <div class="body" role="tabpanel">
    {#if tab === 'backlinks'}
      {#if loading && !linked.length}
        <div class="loading"><PieProgress label={$t('notes.loading')} /></div>
      {:else if groups.length === 0}
        <EmptyState size="compact" title={$t('notes.backlinks.none')} />
      {/if}
      {#each groups as g (g.source)}
        <section class="source">
          <button type="button" class="source-title" on:click={(e) => dispatch('open', { path: g.source, newTab: e.metaKey || e.ctrlKey })}>{noteTitle(g.source)}</button>
          {#each g.lines as m (m.line)}
            <button type="button" class="context" on:click={(e) => dispatch('open', { path: g.source, line: m.line, newTab: e.metaKey || e.ctrlKey })}>{m.text}</button>
          {/each}
        </section>
      {/each}

      <button type="button" class="sub-head" aria-expanded={showUnlinked} on:click={() => (showUnlinked = !showUnlinked)}>
        {$t('notes.backlinks.unlinked')}
        <span class="count">{unlinked.length}</span>
      </button>
      {#if showUnlinked}
        {#if unlinkedGroups.length === 0}
          <EmptyState size="compact" title={$t('notes.backlinks.noneUnlinked')} />
        {/if}
        {#each unlinkedGroups as g (g.source)}
          <section class="source">
            <button type="button" class="source-title" on:click={() => dispatch('open', { path: g.source, newTab: false })}>{noteTitle(g.source)}</button>
            {#each g.lines as m (m.line)}
              <div class="mention">
                <button type="button" class="context" on:click={() => dispatch('open', { path: g.source, line: m.line, newTab: false })}>{m.text}</button>
                <button type="button" class="cal-btn link-btn" on:click={() => void link(m)}>{$t('notes.backlinks.link')}</button>
              </div>
            {/each}
          </section>
        {/each}
      {/if}
    {:else if tab === 'outline'}
      {#if headings.length === 0}
        <EmptyState size="compact" title={$t('notes.outline.none')} detail={$t('notes.outline.noneDetail')} />
      {:else}
        <ul class="outline">
          {#each headings as h (h.line)}
            <li style={`--indent:${h.level - minLevel}`}>
              <button type="button" on:click={() => dispatch('line', h.line)}>{h.text || $t('notes.outline.untitled')}</button>
            </li>
          {/each}
        </ul>
      {/if}
    {:else if tab === 'links'}
      {#if noteLinks.length + objectLinks.length + webLinks.length === 0}
        <EmptyState size="compact" title={$t('notes.links.none')} />
      {/if}
      {#if noteLinks.length}
        <h3 class="sub">{$t('notes.links.notes')}</h3>
        <ul class="links">
          {#each noteLinks as l (`${l.target}#${l.subpath}`)}
            <li>
              <button type="button" class:unresolved={!l.resolved} on:click={(e) => (l.resolved ? dispatch('open', { path: l.resolved, newTab: e.metaKey || e.ctrlKey }) : dispatch('follow', { kind: 'wiki', target: l.target }))}>
                {l.resolved ? noteTitle(l.resolved) : l.target}{l.subpath ? ` > ${l.subpath}` : ''}
              </button>
            </li>
          {/each}
        </ul>
      {/if}
      {#if objectLinks.length}
        <h3 class="sub">{$t('notes.links.pelton')}</h3>
        <ul class="links">
          {#each objectLinks as l (l.target)}
            <li><button type="button" on:click={() => dispatch('follow', { kind: 'url', target: l.target })}>{objectLabel(l.target, l.display)}</button></li>
          {/each}
        </ul>
      {/if}
      {#if webLinks.length}
        <h3 class="sub">{$t('notes.links.web')}</h3>
        <ul class="links">
          {#each webLinks as l (l.target)}
            <li><button type="button" title={l.target} on:click={() => dispatch('follow', { kind: 'url', target: l.target })}>{l.display || l.target}</button></li>
          {/each}
        </ul>
      {/if}
    {:else}
      <div class="graph-tools">
        <span class="depth-label">{$t('notes.graph.depth')}</span>
        <Select value={String(depth)} items={depthItems} ariaLabel={$t('notes.graph.depth')} on:change={(e) => (depth = Number(e.detail))} />
        <label class="check"><input type="checkbox" bind:checked={graphTags} /> {$t('notes.graph.tags')}</label>
      </div>
      <div class="local-graph">
        <NoteGraph {graph} current={path} labelZoom={0.6} label={$t('notes.panel.graph')} on:open={(e) => e.detail.kind === 'note' && dispatch('open', { path: e.detail.id, newTab: e.detail.newTab })} />
      </div>
    {/if}
  </div>
</aside>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--surface-base);
    font-size: var(--fz-list);
  }

  .head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: calc(var(--control-height) + 2 * var(--space-3));
    padding: 0 var(--space-3);
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .tabs {
    display: flex;
    flex: 1;
    gap: var(--space-1);
    min-width: 0;
    overflow-x: auto;
  }

  .tab {
    padding: var(--space-2) var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    white-space: nowrap;
    cursor: var(--cursor-action);
  }

  .tab:hover {
    background: var(--surface-hover);
  }

  .tab[aria-selected='true'] {
    background: var(--selection-bg);
    color: var(--text-primary);
    font-weight: var(--fw-medium);
  }

  .body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-3) var(--space-4) var(--space-5);
  }

  .loading {
    display: flex;
    justify-content: center;
    padding: var(--space-4);
    color: var(--text-tertiary);
  }

  .source {
    margin-bottom: var(--space-4);
  }

  .source-title,
  .context,
  .outline button,
  .links button {
    display: block;
    width: 100%;
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
  }

  .source-title {
    font-weight: var(--fw-semibold);
  }

  .context {
    color: var(--text-secondary);
    font-size: var(--fz-label);
    line-height: var(--list-line-height);
    overflow-wrap: anywhere;
  }

  .source-title:hover,
  .context:hover,
  .outline button:hover,
  .links button:hover {
    background: var(--surface-hover);
  }

  .mention {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
  }

  .link-btn {
    flex-shrink: 0;
    height: var(--chip-height);
    padding: 0 var(--space-3);
    font-size: var(--fz-meta);
  }

  .sub-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    margin-top: var(--space-4);
    padding: var(--space-2) 0;
    border: none;
    border-top: var(--hairline) solid var(--border-subtle);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    cursor: var(--cursor-action);
  }

  .count {
    color: var(--text-tertiary);
    font-weight: var(--fw-regular);
  }

  .sub {
    margin: var(--space-4) 0 var(--space-2);
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    color: var(--text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .outline li {
    padding-inline-start: calc(var(--indent) * var(--space-4));
  }

  .links button.unresolved {
    color: var(--text-tertiary);
  }

  .graph-tools {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    margin-bottom: var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .graph-tools label {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .check input {
    accent-color: var(--accent);
  }

  .local-graph {
    height: calc(var(--cal-sidebar-width) * 1.4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    overflow: hidden;
  }
</style>
