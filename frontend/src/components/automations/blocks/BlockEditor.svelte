<script lang="ts">
  // the block editor of a block script: a palette built from the catalog, the
  // canvas of stacked and nested blocks, and the Lua they compile to. blocks
  // are added by drag, by click or with "/" at the caret, moved by drag or
  // with the keyboard, and every edit can be undone. what each block needs,
  // what is wrong with it and what the last test run did with it shows on the
  // block itself.
  import { createEventDispatcher, onDestroy, onMount, tick } from 'svelte'
  import Live from '../Live.svelte'
  import { derived, writable } from 'svelte/store'
  import undoIcon from '@phosphor-icons/core/assets/duotone/arrow-counter-clockwise-duotone.svg?raw'
  import redoIcon from '@phosphor-icons/core/assets/duotone/arrow-clockwise-duotone.svg?raw'
  import codeIcon from '@phosphor-icons/core/assets/duotone/code-duotone.svg?raw'
  import paletteIcon from '@phosphor-icons/core/assets/duotone/sidebar-simple-duotone.svg?raw'
  import startIcon from '@phosphor-icons/core/assets/duotone/cursor-click-duotone.svg?raw'
  import warnIcon from '@phosphor-icons/core/assets/duotone/warning-circle-duotone.svg?raw'
  import EmptyState from '../../common/EmptyState.svelte'
  import Ph from '../../common/Ph.svelte'
  import BlockList from './BlockList.svelte'
  import InsertMenu from './InsertMenu.svelte'
  import LuaPanel from './LuaPanel.svelte'
  import Palette from './Palette.svelte'
  import RuleChips from './RuleChips.svelte'
  import { t, modSymbol } from '../../../lib/i18n'
  import { entryLabel, permissionDef, serializeTree, type BlockTree, type ScriptCatalog, type ScriptParam } from '../../../lib/scripting'
  import { selectedScriptId, testRun } from '../../../stores/scripts'
  import { currentUIScale } from '../../../theme/theme'
  import { setEditor, type DragPayload, type InsertTarget } from './context'
  import { blockFor, condFor, paletteItems, type PaletteItem } from './palette'
  import { ruleDraft } from './rulefrommail'
  import { runMarks } from './runmarks'
  import { everyEntry, type ScopeContext } from './scope'
  import {
    controlBlock,
    duplicate,
    emptyHistory,
    findBlock,
    flatten,
    indent,
    insertBlock,
    insertCond,
    locate,
    moveBlock,
    moveCond,
    moveDown,
    moveUp,
    outdent,
    parseCondKey,
    parseSlotKey,
    record,
    redo,
    removeBlock,
    rootSlot,
    slotBlocks,
    undo,
    type History,
  } from './tree'
  import { neededPermissions, validate } from './validate'
  import '../../../theme/controls.css'

  /** The block tree being edited. */
  export let tree: BlockTree
  /** The registry everything on screen is built from. */
  export let catalog: ScriptCatalog
  /** The script's trigger id. */
  export let trigger: string
  /** The script's settings, which blocks can refer to. */
  export let settings: ScriptParam[] = []
  /** The permissions the script declares, to tell when a web block still lacks its host. */
  export let declared: string[] = []

  const dispatch = createEventDispatcher<{ change: BlockTree; permissions: string[]; convert: void; addhost: void }>()

  let current: BlockTree = tree
  let history: History = emptyHistory()
  let width = 0
  let narrowAt = 0
  let showLua = false
  let showPalette = true
  let insert: { anchor: HTMLElement; target: InsertTarget } | null = null
  let canvasEl: HTMLDivElement
  let emptyEl: HTMLDivElement
  let moved = ''
  let lastPerms = ''
  let lastProp = serializeTree(tree)

  const treeStore = writable<BlockTree>(current)
  const catalogStore = writable<ScriptCatalog>(catalog)
  const scopeCtx = writable<ScopeContext>({ catalog, trigger, settings })
  const selected = writable<string | null>(null)
  const dropAt = writable<InsertTarget | null>(null)
  const dragging = writable<DragPayload | null>(null)
  const issues = derived([treeStore, scopeCtx], ([tr, ctx]) => validate(tr, ctx))
  const allEntries = derived([treeStore, scopeCtx], ([tr, ctx]) => everyEntry(tr, ctx))
  const hiddenRun = writable(-1)
  const marks = derived([treeStore, testRun, hiddenRun], ([tr, run, hidden]) => (run && run.id !== hidden ? runMarks(tr, run) : new Map()))

  $: syncFrom(tree)
  $: catalogStore.set(catalog)
  $: scopeCtx.set({ catalog, trigger, settings })
  $: items = paletteItems(catalog, trigger, $t)
  $: narrow = width > 0 && narrowAt > 0 && width < narrowAt
  $: perms = neededPermissions(current, catalog, trigger)
  $: if (perms.join(',') !== lastPerms) {
    lastPerms = perms.join(',')
    dispatch('permissions', perms)
  }
  $: risky = flatten(current).filter((b) => b.kind === 'action' && catalog.actions.find((a) => a.id === b.type)?.risky)
  $: needsHost = perms.includes('net') && !declared.some((p) => p.startsWith('net:'))
  $: draft = $ruleDraft && $ruleDraft.scriptId === $selectedScriptId && findBlock(current, $ruleDraft.block) ? $ruleDraft : null
  $: showMarks = !!$testRun && $testRun.id !== $hiddenRun && $marks.size > 0

  onMount(() => {
    narrowAt = parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--block-narrow')) || 0
  })

  // a tree from outside (another script, a save, a reload) replaces the one
  // here and its history; the tree this editor just sent back does not.
  function syncFrom(next: BlockTree): void {
    const s = serializeTree(next)
    if (s === lastProp) {
      return
    }
    lastProp = s
    if (s !== serializeTree(current)) {
      current = next
      history = emptyHistory()
      treeStore.set(current)
    }
  }

  function apply(next: BlockTree, select?: string): void {
    if (next !== current) {
      history = record(history, current)
      current = next
      treeStore.set(current)
      dispatch('change', current)
    }
    if (select !== undefined) {
      selected.set(select)
    }
  }

  function move(next: BlockTree, id: string): void {
    apply(next, id)
    const place = locate(next, id)
    const b = findBlock(next, id)
    if (!place || !b) {
      return
    }
    const total = slotBlocks(next, place.slot)?.length ?? 0
    const name = b.kind === 'action' ? issueLabel(id) : $t(`scripts.blocks.kind.${b.kind}`)
    moved = ''
    void tick().then(() => {
      moved = $t('scripts.blocks.moved').replace('{name}', name).replace('{n}', String(place.index + 1)).replace('{total}', String(total))
    })
  }

  function step(dir: 'undo' | 'redo'): void {
    const r = dir === 'undo' ? undo(history, current) : redo(history, current)
    if (!r) {
      return
    }
    history = r.history
    current = r.tree
    treeStore.set(current)
    dispatch('change', current)
  }

  // where a click in the palette or "/" adds: after the selected block, or
  // at the end of the script.
  function caret(): InsertTarget {
    const id = $selected
    const place = id ? locate(current, id) : null
    return { kind: 'block', place: place ? { slot: place.slot, index: place.index + 1 } : { slot: rootSlot, index: current.blocks.length } }
  }

  function insertAt(target: InsertTarget, item: PaletteItem): void {
    if (item.unavailable) {
      return
    }
    if (target.kind === 'cond') {
      const cond = condFor(item)
      if (cond) {
        apply(insertCond(current, target.group, target.index, cond), target.group.block)
      }
      return
    }
    const block = blockFor(item, current)
    apply(insertBlock(current, target.place, block), block.id)
  }

  function openInsert(anchor: HTMLElement, target: InsertTarget): void {
    insert = { anchor, target }
  }

  function pickInsert(item: PaletteItem): void {
    const target = insert?.target
    insert = null
    if (target) {
      insertAt(target, item)
    }
  }

  function isEditable(el: EventTarget | null): boolean {
    const e = el as HTMLElement | null
    return !!e && (e.tagName === 'INPUT' || e.tagName === 'TEXTAREA' || e.isContentEditable)
  }

  function onEditorKey(e: KeyboardEvent): void {
    const mod = e.metaKey || e.ctrlKey
    if (!mod || isEditable(e.target)) {
      return
    }
    const k = e.key.toLowerCase()
    if (k === 'z' || k === 'y') {
      e.preventDefault()
      e.stopPropagation()
      step(k === 'y' || e.shiftKey ? 'redo' : 'undo')
    }
  }

  function blockKey(e: KeyboardEvent, id: string): void {
    const mod = e.metaKey || e.ctrlKey
    const order = flatten(current)
    const at = order.findIndex((b) => b.id === id)
    let handled = true
    if (e.altKey && e.key === 'ArrowUp') {
      move(moveUp(current, id), id)
    } else if (e.altKey && e.key === 'ArrowDown') {
      move(moveDown(current, id), id)
    } else if (e.altKey && e.key === 'ArrowRight') {
      move(indent(current, id), id)
    } else if (e.altKey && e.key === 'ArrowLeft') {
      move(outdent(current, id), id)
    } else if (e.key === 'ArrowUp' && !mod) {
      selected.set(order[Math.max(0, at - 1)]?.id ?? id)
    } else if (e.key === 'ArrowDown' && !mod) {
      selected.set(order[Math.min(order.length - 1, at + 1)]?.id ?? id)
    } else if (e.key === 'Home') {
      selected.set(order[0]?.id ?? id)
    } else if (e.key === 'End') {
      selected.set(order[order.length - 1]?.id ?? id)
    } else if (mod && e.key.toLowerCase() === 'd') {
      const r = duplicate(current, id)
      apply(r.tree, r.id)
    } else if (e.key === 'Delete' || e.key === 'Backspace') {
      const next = removeBlock(current, id)
      const after = flatten(next)
      apply(next, after[Math.min(at, after.length - 1)]?.id ?? undefined)
      if (after.length === 0) {
        selected.set(null)
      }
    } else if ((e.key === '/' || e.key === '+') && !mod) {
      const place = locate(current, id)
      if (place) {
        openInsert(e.currentTarget as HTMLElement, { kind: 'block', place: { slot: place.slot, index: place.index + 1 } })
      }
    } else if (e.key === 'Enter') {
      const card = e.currentTarget as HTMLElement
      card.querySelector<HTMLElement>('.fields input, .fields select, .fields textarea, .fields button, .branch button, .lua [contenteditable]')?.focus()
    } else {
      handled = false
    }
    if (handled) {
      e.preventDefault()
      e.stopPropagation()
    }
  }

  // --- dragging ------------------------------------------------------------

  let drag: { payload: DragPayload; label: string; x: number; y: number; startX: number; startY: number; moved: boolean } | null = null
  let scale = 1
  let suppressClick = false

  function startDrag(event: PointerEvent, payload: DragPayload, label: string): void {
    if (event.button !== 0 || drag) {
      return
    }
    event.preventDefault()
    scale = currentUIScale()
    drag = { payload, label, x: event.clientX, y: event.clientY, startX: event.clientX, startY: event.clientY, moved: false }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
    window.addEventListener('keydown', onDragKey, true)
  }

  function takesConds(p: DragPayload): boolean {
    return p.kind === 'cond' || (p.kind === 'palette' && condFor(p.item) !== null)
  }

  function takesBlocks(p: DragPayload): boolean {
    return p.kind === 'block' || p.kind === 'palette'
  }

  function indexIn(list: Element, selector: string, y: number): number {
    const children = [...list.children].filter((c) => c.matches(selector))
    const i = children.findIndex((c) => {
      const r = c.getBoundingClientRect()
      return y < r.top + r.height / 2
    })
    return i < 0 ? children.length : i
  }

  function result(p: DragPayload, target: InsertTarget): BlockTree | null {
    if (p.kind === 'block') {
      return target.kind === 'block' ? moveBlock(current, p.id, target.place) : null
    }
    if (p.kind === 'cond') {
      return target.kind === 'cond' ? moveCond(current, p.cp, target.group, target.index) : null
    }
    return target.kind === 'cond' && !condFor(p.item) ? null : current
  }

  function targetAt(x: number, y: number, p: DragPayload): InsertTarget | null {
    const under = document.elementFromPoint(x, y)
    if (!under || !canvasEl?.contains(under)) {
      return null
    }
    let target: InsertTarget | null = null
    const group = takesConds(p) ? under.closest<HTMLElement>('[data-cond-group]') : null
    if (group?.dataset.condGroup) {
      target = { kind: 'cond', group: parseCondKey(group.dataset.condGroup), index: indexIn(group, '[data-cond-index]', y) }
    } else if (takesBlocks(p)) {
      const slot = under.closest<HTMLElement>('[data-block-slot]') ?? canvasEl.querySelector<HTMLElement>('[data-block-slot="/body"]')
      if (slot?.dataset.blockSlot) {
        target = { kind: 'block', place: { slot: parseSlotKey(slot.dataset.blockSlot), index: indexIn(slot, '[data-block-index]', y) } }
      }
    }
    return target && result(p, target) ? target : null
  }

  function onMove(event: PointerEvent): void {
    if (!drag) {
      return
    }
    const moved = drag.moved || Math.hypot(event.clientX - drag.startX, event.clientY - drag.startY) > 4
    drag = { ...drag, x: event.clientX, y: event.clientY, moved }
    if (!moved) {
      return
    }
    dragging.set(drag.payload)
    dropAt.set(targetAt(event.clientX, event.clientY, drag.payload))
    const scroller = scrollerOf(canvasEl)
    if (scroller) {
      const r = scroller.getBoundingClientRect()
      const edge = r.height / 10
      if (event.clientY < r.top + edge) {
        scroller.scrollTop -= Math.ceil((r.top + edge - event.clientY) / 4)
      } else if (event.clientY > r.bottom - edge) {
        scroller.scrollTop += Math.ceil((event.clientY - (r.bottom - edge)) / 4)
      }
    }
  }

  function scrollerOf(el: HTMLElement | null): HTMLElement | null {
    for (let p = el?.parentElement ?? null; p; p = p.parentElement) {
      if (p.scrollHeight > p.clientHeight && /auto|scroll/.test(getComputedStyle(p).overflowY)) {
        return p
      }
    }
    return null
  }

  function endDrag(): void {
    window.removeEventListener('pointermove', onMove)
    window.removeEventListener('pointerup', onUp)
    window.removeEventListener('keydown', onDragKey, true)
    drag = null
    dragging.set(null)
    dropAt.set(null)
  }

  function onDragKey(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      endDrag()
    }
  }

  function onUp(): void {
    const d = drag
    const target = $dropAt
    endDrag()
    if (!d?.moved) {
      return
    }
    suppressClick = true
    setTimeout(() => (suppressClick = false), 0)
    if (!target) {
      return
    }
    if (d.payload.kind === 'palette') {
      insertAt(target, d.payload.item)
      return
    }
    const next = result(d.payload, target)
    if (next && d.payload.kind === 'block') {
      move(next, d.payload.id)
    } else if (next) {
      apply(next, d.payload.kind === 'cond' ? d.payload.cp.block : undefined)
    }
  }

  function addFromPalette(item: PaletteItem): void {
    if (!suppressClick) {
      insertAt(caret(), item)
    }
  }

  onDestroy(endDrag)

  setEditor({
    tree: treeStore,
    catalog: catalogStore,
    scopeCtx,
    allEntries,
    issues,
    marks,
    selected,
    dropAt,
    dragging,
    apply,
    move,
    startDrag,
    openInsert,
    blockKey,
  })

  function issueLabel(block: string): string {
    const b = findBlock(current, block)
    if (!b) {
      return block
    }
    if (b.kind === 'action') {
      const def = catalog.actions.find((a) => a.id === b.type)
      return def ? entryLabel($t, def) : $t('scripts.blocks.unknownBlock')
    }
    return $t(`scripts.blocks.kind.${b.kind}`)
  }
</script>

<!-- svelte-ignore a11y-no-static-element-interactions -->
<div class="block-editor" class:narrow class:with-lua={showLua} class:no-palette={!showPalette} bind:clientWidth={width} on:keydown={onEditorKey}>
  <div class="toolbar" role="toolbar" aria-label={$t('scripts.blocks.toolbar')}>
    <button type="button" class="icon-btn" disabled={history.past.length === 0} aria-label={$t('scripts.blocks.undo')} title={`${$t('scripts.blocks.undo')} (${modSymbol}Z)`} on:click={() => step('undo')}>
      <Ph svg={undoIcon} size={15} />
    </button>
    <button type="button" class="icon-btn" disabled={history.future.length === 0} aria-label={$t('scripts.blocks.redo')} title={`${$t('scripts.blocks.redo')} (${modSymbol}Y)`} on:click={() => step('redo')}>
      <Ph svg={redoIcon} size={15} />
    </button>
    <span class="sep" aria-hidden="true"></span>
    <div class="perms" aria-label={$t('scripts.blocks.needs')} role="group">
      {#if perms.length > 0}
        <span class="perms-label">{$t('scripts.blocks.needs')}</span>
        {#each perms as p (p)}
          {@const def = permissionDef(catalog, p)}
          <span class="perm risk-{def?.risk ?? 'low'}" title={def ? $t(`scripts.risk.${def.risk}`) : p}>{def ? entryLabel($t, def) : p}</span>
        {/each}
        {#if risky.length > 0}
          <span class="perm risk-high">{$t('scripts.blocks.riskyCount').replace('{n}', String(risky.length))}</span>
        {/if}
      {/if}
    </div>
    <button type="button" class="icon-btn" class:on={showPalette} hidden={narrow} aria-pressed={showPalette} aria-label={$t('scripts.blocks.palette')} title={$t('scripts.blocks.palette')} on:click={() => (showPalette = !showPalette)}>
      <Ph svg={paletteIcon} size={15} />
    </button>
    <button type="button" class="icon-btn text" class:on={showLua} aria-pressed={showLua} on:click={() => (showLua = !showLua)}>
      <Ph svg={codeIcon} size={15} />{$t('scripts.blocks.lua')}
    </button>
  </div>

  {#if needsHost}
    <p class="host-note" role="note">
      {$t('scripts.blocks.netHost')}
      <button type="button" class="link" on:click={() => dispatch('addhost')}>{$t('scripts.blocks.netHostAdd')}</button>
    </p>
  {/if}

  {#if draft}
    <RuleChips {draft} />
  {/if}

  {#if $issues.length > 0}
    <details class="issues">
      <summary>
        <Ph svg={warnIcon} size={14} />
        {$t($issues.length === 1 ? 'scripts.blocks.issues.one' : 'scripts.blocks.issues.many').replace('{n}', String($issues.length))}
      </summary>
      <ul>
        {#each $issues as issue, i (i)}
          <li>
            <button type="button" class="link" on:click={() => selected.set(issue.block)}>{issueLabel(issue.block)}</button>:
            {$t(`scripts.blocks.issue.${issue.code}`)}
          </li>
        {/each}
      </ul>
    </details>
  {/if}

  {#if showMarks && $testRun}
    <p class="run-note" role="status">
      {$t('scripts.blocks.runShown').replace('{status}', $t(`scripts.status.${$testRun.status}`))}
      <button type="button" class="link" on:click={() => hiddenRun.set($testRun?.id ?? -1)}>{$t('scripts.blocks.runHide')}</button>
    </p>
  {/if}

  <Live text={moved} />
  <div class="body">
    {#if showPalette && !narrow}
      <Palette {items} on:add={(e) => addFromPalette(e.detail)} />
    {/if}
    <div class="canvas" bind:this={canvasEl} aria-label={$t('scripts.blocks')} role="region">
      {#if current.blocks.length === 0}
        <div class="empty" bind:this={emptyEl}>
          <EmptyState
            size="compact"
            icon={startIcon}
            title={$t('scripts.blocks.empty.title')}
            detail={$t('scripts.blocks.empty.detail')}
            actionLabel={$t('scripts.blocks.empty.add')}
            onAction={() => openInsert(emptyEl, { kind: 'block', place: { slot: rootSlot, index: 0 } })}
            secondaryLabel={$t('scripts.blocks.empty.if')}
            onSecondary={() => {
              const b = controlBlock(current, 'if')
              apply(insertBlock(current, { slot: rootSlot, index: 0 }, b), b.id)
            }}
          />
        </div>
      {/if}
      <BlockList slot={rootSlot} blocks={current.blocks} label={$t('scripts.blocks')} />
      <p class="keys">{$t('scripts.blocks.keys').replace('{mod}', modSymbol)}</p>
    </div>
    {#if showLua}
      <div class="lua">
        <LuaPanel tree={current} {trigger} selected={$selected} on:select={(e) => selected.set(e.detail)} on:convert={() => dispatch('convert')} />
      </div>
    {/if}
  </div>
</div>

{#if insert}
  <InsertMenu anchor={insert.anchor} {items} conditionsOnly={insert.target.kind === 'cond'} on:pick={(e) => pickInsert(e.detail)} on:close={() => (insert = null)} />
{/if}

{#if drag?.moved}
  <div class="ghost" style={`left:${drag.x / scale}px;top:${drag.y / scale}px`} aria-hidden="true">{drag.label}</div>
{/if}

<style>
  .block-editor {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    min-width: 0;
  }

  .toolbar {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    min-height: var(--control-height);
  }

  .sep {
    width: var(--hairline);
    align-self: stretch;
    margin: var(--space-1) var(--space-1);
    background: var(--border-subtle);
  }

  .icon-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    min-width: var(--control-height);
    height: var(--control-height);
    justify-content: center;
    padding: 0 var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .icon-btn[hidden] {
    display: none;
  }

  .icon-btn:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .icon-btn.on {
    background: var(--selection-bg);
    color: var(--accent);
  }

  .icon-btn:disabled {
    opacity: 0.4;
    cursor: default;
  }

  .icon-btn:focus-visible,
  .link:focus-visible,
  .issues summary:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .perms {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1);
  }

  .perms-label {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .perm {
    padding: 0 var(--space-1);
    border: var(--hairline) solid currentColor;
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
    line-height: var(--chip-height);
    color: var(--text-tertiary);
  }

  .perm.risk-medium {
    color: var(--warning);
  }

  .perm.risk-high {
    color: var(--danger);
  }

  .issues {
    padding: var(--space-2) var(--space-3);
    border-inline-start: 2px solid var(--danger);
    background: var(--danger-bg);
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
  }

  .issues summary {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--danger);
    cursor: var(--cursor-action);
  }

  .issues ul {
    margin: var(--space-2) 0 0;
    padding-inline-start: var(--space-4);
  }

  .host-note {
    margin: 0;
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    align-items: center;
    padding: var(--space-2) var(--space-3);
    border-inline-start: 2px solid var(--warning);
    background: var(--warning-bg);
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
  }

  .run-note {
    margin: 0;
    display: flex;
    gap: var(--space-2);
    align-items: center;
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .link {
    padding: 0;
    border: none;
    background: none;
    color: var(--link);
    font: inherit;
    text-decoration: underline;
    cursor: var(--cursor-action);
  }

  .body {
    display: grid;
    grid-template-columns: var(--block-palette-width) minmax(0, 1fr);
    gap: var(--space-4);
    min-height: calc(var(--control-height) * 8);
  }

  .no-palette .body,
  .narrow .body {
    grid-template-columns: minmax(0, 1fr);
  }

  .with-lua .body {
    grid-template-columns: var(--block-palette-width) minmax(0, 1fr) var(--block-lua-width);
  }

  .with-lua.no-palette .body {
    grid-template-columns: minmax(0, 1fr) var(--block-lua-width);
  }

  .with-lua.narrow .body {
    grid-template-columns: minmax(0, 1fr);
  }

  .body > :global(.palette) {
    position: sticky;
    top: 0;
    align-self: start;
    max-height: calc(var(--control-height) * 18);
  }

  .canvas {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    min-width: 0;
    min-height: 0;
  }

  .empty {
    padding: var(--space-4) 0;
  }

  .keys {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .lua {
    display: flex;
    min-width: 0;
    max-height: calc(var(--control-height) * 18);
  }

  .lua > :global(*) {
    flex: 1;
  }

  .ghost {
    position: fixed;
    z-index: 900;
    max-width: calc(var(--control-height) * 8);
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-inline-start: 3px solid var(--accent);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    font-size: var(--fz-list);
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    pointer-events: none;
    transform: translate(var(--space-3), var(--space-3));
  }
</style>
