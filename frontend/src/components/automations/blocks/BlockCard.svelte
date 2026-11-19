<script lang="ts">
  // one block on the canvas: its title, what it needs and how risky it is,
  // what the last test run made of it, its problems, its fields and, for an if
  // or a for each, the blocks inside. a kind or action this editor does not
  // know shows as an unknown block that keeps every field and still moves.
  import { onDestroy } from 'svelte'
  import gripIcon from '@phosphor-icons/core/assets/duotone/dots-six-vertical-duotone.svg?raw'
  import actionIcon from '@phosphor-icons/core/assets/duotone/lightning-duotone.svg?raw'
  import ifIcon from '@phosphor-icons/core/assets/duotone/git-branch-duotone.svg?raw'
  import loopIcon from '@phosphor-icons/core/assets/duotone/repeat-duotone.svg?raw'
  import setIcon from '@phosphor-icons/core/assets/duotone/equals-duotone.svg?raw'
  import stopIcon from '@phosphor-icons/core/assets/duotone/stop-circle-duotone.svg?raw'
  import luaIcon from '@phosphor-icons/core/assets/duotone/code-duotone.svg?raw'
  import unknownIcon from '@phosphor-icons/core/assets/duotone/puzzle-piece-duotone.svg?raw'
  import upIcon from '@phosphor-icons/core/assets/duotone/arrow-up-duotone.svg?raw'
  import downIcon from '@phosphor-icons/core/assets/duotone/arrow-down-duotone.svg?raw'
  import indentIcon from '@phosphor-icons/core/assets/duotone/text-indent-duotone.svg?raw'
  import outdentIcon from '@phosphor-icons/core/assets/duotone/text-outdent-duotone.svg?raw'
  import copyIcon from '@phosphor-icons/core/assets/duotone/copy-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import riskIcon from '@phosphor-icons/core/assets/duotone/shield-warning-duotone.svg?raw'
  import xIcon from '@phosphor-icons/core/assets/duotone/x-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import ArgField from './ArgField.svelte'
  import BlockList from './BlockList.svelte'
  import CondView from './CondView.svelte'
  import LuaEditor from '../LuaEditor.svelte'
  import { t } from '../../../lib/i18n'
  import { entryDescription, entryLabel, needs, permissionDef, type Block, type BlockArg, type ScriptParam } from '../../../lib/scripting'
  import { getEditor } from './context'
  import { scopeAt } from './scope'
  import {
    addBranch,
    addElse,
    duplicate,
    flatten,
    indent,
    locate,
    moveDown,
    moveUp,
    outdent,
    removeBlock,
    removeBranch,
    removeElse,
    setArgIn,
    slotBlocks,
    updateBlock,
  } from './tree'
  import { varNamePattern } from './validate'
  import '../../../theme/controls.css'

  /** The block. */
  export let block: Block

  const ed = getEditor()
  const { tree, catalog, scopeCtx, issues, marks, selected, dragging } = ed
  const icons: Record<string, string> = { action: actionIcon, if: ifIcon, foreach: loopIcon, set: setIcon, stop: stopIcon, lua: luaIcon }
  const listParam: ScriptParam = { name: 'list', type: 'list', of: 'string', labelKey: 'scripts.blocks.foreach.list' }
  const valueParam: ScriptParam = { name: 'value', type: 'string', labelKey: 'scripts.blocks.set.value' }

  let el: HTMLDivElement
  let luaTimer: ReturnType<typeof setTimeout> | undefined

  $: id = block.id
  $: def = block.kind === 'action' ? $catalog.actions.find((a) => a.id === block.type) : undefined
  $: known = block.kind === 'action' ? !!def : block.kind in icons
  $: title = !known ? $t('scripts.blocks.unknownBlock') : def ? entryLabel($t, def) : $t(`scripts.blocks.kind.${block.kind}`)
  $: desc = def ? entryDescription($t, def) : ''
  $: perms = needs(def).flatMap((p) => permissionDef($catalog, p) ?? [])
  $: scope = scopeAt($tree, id, $scopeCtx)
  $: mine = $issues.filter((i) => i.block === id)
  $: ownIssues = mine.filter((i) => !i.cond && !i.param)
  $: mark = $marks.get(id)
  $: isSelected = $selected === id
  $: order = flatten($tree)
  $: tabbable = isSelected || ($selected === null && order[0]?.id === id) || ($selected !== null && !order.some((b) => b.id === $selected) && order[0]?.id === id)
  $: place = locate($tree, id)
  $: siblings = place ? (slotBlocks($tree, place.slot)?.length ?? 0) : 0
  $: canUp = !!place && (place.index > 0 || place.slot.parent !== null)
  $: canDown = !!place && (place.index < siblings - 1 || place.slot.parent !== null)
  $: canIndent = indent($tree, id) !== $tree
  $: canOutdent = !!place && place.slot.parent !== null
  $: beingDragged = $dragging?.kind === 'block' && $dragging.id === id
  $: varOk = varNamePattern.test(block.as ?? '')
  $: issueText =mine.map((i) => $t(`scripts.blocks.issue.${i.code}`)).join('. ')
  $: if (isSelected && el && document.activeElement !== el && !el.contains(document.activeElement)) {
    el.focus({ preventScroll: false })
  }

  onDestroy(() => clearTimeout(luaTimer))

  function paramProblem(name: string): string {
    const issue = mine.find((i) => !i.cond && i.param === name)
    return issue ? $t(`scripts.blocks.issue.${issue.code}`) : ''
  }

  function edit(fn: (b: Block) => void): void {
    ed.apply(updateBlock($tree, id, fn))
  }

  function setArg(name: string, arg: BlockArg | undefined): void {
    edit((b) => (b.args = setArgIn(b.args, name, arg)))
  }

  function setOwn(field: 'list' | 'value', arg: BlockArg | undefined): void {
    edit((b) => {
      if (arg) {
        b[field] = arg
      } else {
        delete b[field]
      }
    })
  }

  function setCode(code: string): void {
    clearTimeout(luaTimer)
    luaTimer = setTimeout(() => edit((b) => (b.code = code)), 400)
  }

  function remove(): void {
    const at = order.findIndex((b) => b.id === id)
    const next = removeBlock($tree, id)
    const after = flatten(next)
    ed.apply(next, after[Math.min(at, after.length - 1)]?.id)
  }

  function dup(): void {
    const r = duplicate($tree, id)
    ed.apply(r.tree, r.id)
  }

  function onKey(e: KeyboardEvent): void {
    if (e.target === el) {
      ed.blockKey(e, id)
    }
  }
</script>

<!-- svelte-ignore a11y-no-noninteractive-tabindex a11y-no-noninteractive-element-interactions -->
<div
  bind:this={el}
  class="card kind-{known ? block.kind : 'unknown'}"
  class:selected={isSelected}
  class:has-issues={mine.length > 0}
  class:dragging={beingDragged}
  class:mark-ran={mark?.status === 'ran'}
  class:mark-planned={mark?.status === 'planned'}
  class:mark-error={mark?.status === 'error'}
  class:mark-skipped={mark?.status === 'skipped'}
  data-block-id={id}
  role="group"
  aria-roledescription={$t('scripts.blocks.block')}
  aria-label={issueText ? `${title}. ${issueText}` : title}
  tabindex={tabbable ? 0 : -1}
  on:keydown={onKey}
  on:focusin={() => selected.set(id)}
>
  <div class="head">
    <span class="grip" aria-hidden="true" title={$t('scripts.blocks.drag')} on:pointerdown={(e) => ed.startDrag(e, { kind: 'block', id }, title)}><Ph svg={gripIcon} size={14} /></span>
    <span class="icon" aria-hidden="true"><Ph svg={known ? icons[block.kind] : unknownIcon} size={15} /></span>
    <span class="title" title={desc || undefined}>{title}</span>
    {#if !known}<code class="raw">{block.kind === 'action' ? block.type : block.kind}</code>{/if}
    {#each perms as perm (perm.id)}
      <span class="badge risk-{perm.risk}" title={entryDescription($t, perm)}>{entryLabel($t, perm)}</span>
    {/each}
    {#if def?.risky}
      <span class="badge risky" title={$t('scripts.blocks.risky.desc')}><Ph svg={riskIcon} size={12} />{$t('scripts.blocks.risky')}</span>
    {/if}
    {#if mark}
      <span class="badge run {mark.status}" role="status">{$t(`scripts.blocks.mark.${mark.status}`)}</span>
    {/if}
    {#if mine.length > 0}
      <span class="badge issues" title={issueText}>{$t('scripts.blocks.issueCount').replace('{n}', String(mine.length))}</span>
    {/if}
    <span class="tools">
      <button type="button" class="tool" disabled={!canUp} aria-label={$t('scripts.blocks.moveUp')} title={$t('scripts.blocks.moveUp')} on:click={() => ed.move(moveUp($tree, id), id)}><Ph svg={upIcon} size={13} /></button>
      <button type="button" class="tool" disabled={!canDown} aria-label={$t('scripts.blocks.moveDown')} title={$t('scripts.blocks.moveDown')} on:click={() => ed.move(moveDown($tree, id), id)}><Ph svg={downIcon} size={13} /></button>
      <button type="button" class="tool" disabled={!canOutdent} aria-label={$t('scripts.blocks.outdent')} title={$t('scripts.blocks.outdent')} on:click={() => ed.move(outdent($tree, id), id)}><Ph svg={outdentIcon} size={13} /></button>
      <button type="button" class="tool" disabled={!canIndent} aria-label={$t('scripts.blocks.indent')} title={$t('scripts.blocks.indent')} on:click={() => ed.move(indent($tree, id), id)}><Ph svg={indentIcon} size={13} /></button>
      <button type="button" class="tool" aria-label={$t('scripts.blocks.duplicate')} title={$t('scripts.blocks.duplicate')} on:click={dup}><Ph svg={copyIcon} size={13} /></button>
      <button type="button" class="tool danger" aria-label={$t('scripts.blocks.delete')} title={$t('scripts.blocks.delete')} on:click={remove}><Ph svg={trashIcon} size={13} /></button>
    </span>
  </div>

  {#if mark && (mark.lines.length > 0 || mark.taken)}
    <ul class="run-lines">
      {#each mark.taken ?? [] as b (b)}
        <li>{b < 0 ? $t(block.else?.length ? 'scripts.blocks.mark.else' : 'scripts.blocks.mark.none') : $t('scripts.blocks.mark.branch').replace('{n}', String(b + 1))}</li>
      {/each}
      {#each mark.lines as line, i (i)}<li>{line}</li>{/each}
    </ul>
  {/if}
  {#if ownIssues.length > 0}
    <ul class="problems">
      {#each ownIssues as issue, i (i)}
        <li>{$t(`scripts.blocks.issue.${issue.code}`)}{issue.detail && issue.code !== 'badRef' ? `: ${issue.detail}` : ''}</li>
      {/each}
    </ul>
  {/if}

  {#if !known}
    <p class="note">{$t('scripts.blocks.unknownKept')}</p>
  {:else if block.kind === 'action' && def}
    {#if def.params.length > 0}
      <div class="fields">
        {#each def.params as p (p.name)}
          <ArgField param={p} arg={block.args?.[p.name]} {scope} problem={paramProblem(p.name)} idBase={`b-${id}`} on:change={(e) => setArg(p.name, e.detail)} />
        {/each}
      </div>
    {/if}
    {#if def.outputs && def.outputs.length > 0}
      <p class="outputs">
        {$t('scripts.blocks.outputs')}
        {#each def.outputs as o (o.name)}<span class="out">{entryLabel($t, o)}</span>{/each}
      </p>
    {/if}
  {:else if block.kind === 'if'}
    {#each block.branches ?? [] as branch, i (i)}
      <section class="branch" aria-label={$t(i === 0 ? 'scripts.blocks.if' : 'scripts.blocks.elseIf')}>
        <div class="branch-head">
          <span class="word">{$t(i === 0 ? 'scripts.blocks.if' : 'scripts.blocks.elseIf')}</span>
          {#if i > 0}
            <button type="button" class="tool" aria-label={$t('scripts.blocks.removeBranch')} title={$t('scripts.blocks.removeBranch')} on:click={() => ed.apply(removeBranch($tree, id, i))}><Ph svg={xIcon} size={12} /></button>
          {/if}
        </div>
        <CondView cond={branch.when} cp={{ block: id, branch: i, path: [] }} {scope} />
        <span class="word then">{$t('scripts.blocks.then')}</span>
        <BlockList slot={{ parent: id, key: i }} blocks={branch.body ?? []} label={`${title} ${i + 1}`} />
      </section>
    {/each}
    {#if block.else}
      <section class="branch" aria-label={$t('scripts.blocks.else')}>
        <div class="branch-head">
          <span class="word">{$t('scripts.blocks.else')}</span>
          <button type="button" class="tool" aria-label={$t('scripts.blocks.removeElse')} title={$t('scripts.blocks.removeElse')} on:click={() => ed.apply(removeElse($tree, id))}><Ph svg={xIcon} size={12} /></button>
        </div>
        <BlockList slot={{ parent: id, key: 'else' }} blocks={block.else} label={$t('scripts.blocks.else')} />
      </section>
    {/if}
    <div class="branch-actions">
      <button type="button" class="cal-btn small" on:click={() => ed.apply(addBranch($tree, id))}>{$t('scripts.blocks.addElseIf')}</button>
      {#if !block.else}
        <button type="button" class="cal-btn small" on:click={() => ed.apply(addElse($tree, id))}>{$t('scripts.blocks.addElse')}</button>
      {/if}
    </div>
  {:else if block.kind === 'foreach'}
    <div class="fields">
      <label class="var">
        <span class="label">{$t('scripts.blocks.foreach.as')}</span>
        <input class="cal-field mono" value={block.as ?? ''} aria-invalid={!varOk} aria-describedby={varOk ? undefined : `b-${id}-as-problem`} spellcheck="false" on:change={(e) => edit((b) => (b.as = e.currentTarget.value.trim()))} />
        {#if !varOk}<span class="var-problem" id={`b-${id}-as-problem`}>{$t('scripts.blocks.issue.noVar')}</span>{/if}
      </label>
      <ArgField param={listParam} arg={block.list} {scope} problem={paramProblem('list')} idBase={`b-${id}`} on:change={(e) => setOwn('list', e.detail)} />
    </div>
    <BlockList slot={{ parent: id, key: 'body' }} blocks={block.body ?? []} label={title} />
  {:else if block.kind === 'set'}
    <div class="fields">
      <label class="var">
        <span class="label">{$t('scripts.blocks.set.as')}</span>
        <input class="cal-field mono" value={block.as ?? ''} aria-invalid={!varOk} aria-describedby={varOk ? undefined : `b-${id}-as-problem`} spellcheck="false" on:change={(e) => edit((b) => (b.as = e.currentTarget.value.trim()))} />
        {#if !varOk}<span class="var-problem" id={`b-${id}-as-problem`}>{$t('scripts.blocks.issue.noVar')}</span>{/if}
      </label>
      <ArgField param={valueParam} arg={block.value} {scope} problem={paramProblem('value')} idBase={`b-${id}`} on:change={(e) => setOwn('value', e.detail)} />
    </div>
  {:else if block.kind === 'stop'}
    <p class="note">{$t('scripts.blocks.kind.stop.desc')}</p>
  {:else if block.kind === 'lua'}
    <div class="lua">
      <LuaEditor label={$t('scripts.blocks.kind.lua')} content={block.code ?? ''} on:change={(e) => setCode(e.detail)} />
    </div>
  {/if}
</div>

<style>
  .card {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-inline-start: 3px solid var(--border-strong);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    transition:
      border-color var(--motion-fast) var(--ease-out),
      opacity var(--motion-fast) var(--ease-out);
  }

  .card.kind-action {
    border-inline-start-color: var(--accent);
  }

  .card.kind-if,
  .card.kind-foreach {
    border-inline-start-color: color-mix(in srgb, var(--accent) 45%, var(--border-strong));
  }

  .card.kind-unknown {
    border-inline-start-color: var(--warning);
    background: var(--warning-bg);
  }

  .card.has-issues {
    border-inline-start-color: var(--danger);
  }

  .card.selected {
    border-color: var(--accent);
  }

  .card:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .card.dragging {
    opacity: 0.4;
  }

  .card.mark-skipped {
    opacity: 0.6;
  }

  .head {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--space-2);
    min-height: var(--control-height);
  }

  .grip {
    display: inline-flex;
    margin-inline-start: calc(-1 * var(--space-2));
    color: var(--text-tertiary);
    cursor: grab;
    touch-action: none;
  }

  .icon {
    display: inline-flex;
    color: var(--text-secondary);
  }

  .kind-action .icon {
    color: var(--accent);
  }

  .title {
    font-size: var(--fz-body);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .raw {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .badge {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: 0 var(--space-1);
    border: var(--hairline) solid currentColor;
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
    line-height: var(--chip-height);
    color: var(--text-tertiary);
  }

  .badge.risk-medium,
  .badge.run.planned {
    color: var(--warning);
  }

  .badge.risk-high,
  .badge.risky,
  .badge.issues,
  .badge.run.error {
    color: var(--danger);
  }

  .badge.run.ran {
    color: var(--success);
  }

  .tools {
    display: inline-flex;
    gap: var(--space-1);
    margin-inline-start: auto;
    opacity: 0;
  }

  .card:hover > .head > .tools,
  .card.selected > .head > .tools,
  .card:focus-within > .head > .tools {
    opacity: 1;
  }

  .tool::after {
    content: '';
    position: absolute;
    top: 50%;
    left: 50%;
    width: max(100%, var(--target-min));
    height: max(100%, var(--target-min));
    transform: translate(-50%, -50%);
  }

  .tool {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--chip-height);
    height: var(--chip-height);
    padding: 0;
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
  }

  .tool:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .tool.danger:hover:not(:disabled) {
    color: var(--danger);
  }

  .tool:disabled {
    opacity: 0.35;
    cursor: default;
  }

  .tool:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .fields {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(calc(var(--control-height) * 5), 1fr));
    gap: var(--space-2) var(--space-3);
  }

  .var {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .label {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
    line-height: var(--chip-height);
  }

  .var-problem {
    font-size: var(--fz-meta);
    color: var(--danger);
  }

  .mono {
    font-family: var(--font-mono);
  }

  .branch {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .branch-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .word {
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    letter-spacing: 0.02em;
    color: var(--text-secondary);
    text-transform: uppercase;
  }

  .branch-actions {
    display: flex;
    gap: var(--space-1);
  }

  .outputs {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1);
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .out {
    padding: 0 var(--space-1);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-family: var(--font-mono);
  }

  .note {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .problems,
  .run-lines {
    margin: 0;
    padding-inline-start: var(--space-4);
    font-size: var(--fz-meta);
  }

  .problems {
    color: var(--danger);
  }

  .run-lines {
    color: var(--text-secondary);
    font-family: var(--font-mono);
  }

  .lua {
    display: flex;
    min-height: calc(var(--control-height) * 4);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    overflow: hidden;
  }
</style>
