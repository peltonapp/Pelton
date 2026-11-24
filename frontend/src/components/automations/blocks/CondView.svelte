<script lang="ts">
  // one condition of an if branch: a registered condition with its fields, or
  // an all, any or not group holding more. groups nest; every condition can be
  // dragged by its grip, moved with its buttons, wrapped in a group or removed.
  import gripIcon from '@phosphor-icons/core/assets/duotone/dots-six-vertical-duotone.svg?raw'
  import upIcon from '@phosphor-icons/core/assets/duotone/arrow-up-duotone.svg?raw'
  import downIcon from '@phosphor-icons/core/assets/duotone/arrow-down-duotone.svg?raw'
  import groupIcon from '@phosphor-icons/core/assets/duotone/tree-structure-duotone.svg?raw'
  import notIcon from '@phosphor-icons/core/assets/duotone/prohibit-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import plusIcon from '@phosphor-icons/core/assets/duotone/plus-duotone.svg?raw'
  import unknownIcon from '@phosphor-icons/core/assets/duotone/puzzle-piece-duotone.svg?raw'
  import testIcon from '@phosphor-icons/core/assets/duotone/test-tube-duotone.svg?raw'
  import Modal from '../../common/Modal.svelte'
  import Ph from '../../common/Ph.svelte'
  import Select from '../../common/Select.svelte'
  import MatchTester from '../MatchTester.svelte'
  import ArgField from './ArgField.svelte'
  import { t } from '../../../lib/i18n'
  import { entryLabel, needs, type BlockArg, type BlockCond } from '../../../lib/scripting'
  import { getEditor } from './context'
  import { onTestedMessage, testableOnMail } from './matchcond'
  import type { ScopeEntry } from './scope'
  import { condKey, isGroup, removeCond, setArgIn, setCond, shiftCond, unwrapCond, updateCond, wrapCond, type CondPath } from './tree'
  import '../../../theme/controls.css'

  /** The condition. */
  export let cond: BlockCond
  /** Where it sits. */
  export let cp: CondPath
  /** The values in scope at the if block. */
  export let scope: ScopeEntry[]
  /** How many siblings it has, for the move buttons. */
  export let siblings = 1

  const ed = getEditor()
  const { tree, catalog, issues, dropAt, dragging } = ed

  let addButton: HTMLButtonElement
  let testing = false

  $: key = condKey(cp)
  $: isRoot = cp.path.length === 0
  $: index = cp.path[cp.path.length - 1] ?? 0
  $: def = cond.kind === 'condition' ? $catalog.conditions.find((c) => c.id === cond.type) : undefined
  $: group = isGroup(cond)
  $: known = group || !!def
  $: title = group ? $t(`scripts.blocks.group.${cond.kind}`) : def ? entryLabel($t, def) : $t('scripts.blocks.unknownCondition')
  $: mine = $issues.filter((i) => i.cond && condKey(i.cond) === key)
  $: groupIssue = mine.find((i) => !i.param)
  $: items = cond.items ?? []
  $: drop = $dropAt?.kind === 'cond' && condKey($dropAt.group) === key ? $dropAt.index : -1
  $: beingDragged = $dragging?.kind === 'cond' && condKey($dragging.cp) === key
  $: testable = testableOnMail(cond, $catalog)
  $: kindItems = (['all', 'any', 'not'] as const).map((k) => ({ value: k, label: $t(`scripts.blocks.group.${k}`) }))

  function setArg(name: string, arg: BlockArg | undefined): void {
    ed.apply(updateCond($tree, cp, (c) => (c.args = setArgIn(c.args, name, arg))))
  }

  function setKind(kind: string): void {
    ed.apply(setCond($tree, cp, { ...cond, kind: kind as BlockCond['kind'] }))
  }

  function paramProblem(name: string): string {
    const issue = mine.find((i) => i.param === name)
    return issue ? $t(`scripts.blocks.issue.${issue.code}`) : ''
  }
</script>

<div class="cond" class:group class:root={isRoot} class:unknown={!known} class:dragging={beingDragged} data-cond-key={key} role="group" aria-label={title}>
  <div class="head">
    {#if !isRoot}
      <span class="grip" aria-hidden="true" on:pointerdown={(e) => ed.startDrag(e, { kind: 'cond', cp }, title)}><Ph svg={gripIcon} size={14} /></span>
    {/if}
    {#if group}
      <Select ariaLabel={$t('scripts.blocks.group.kind')} value={cond.kind} items={kindItems} on:change={(e) => setKind(e.detail)} />
    {:else}
      {#if !known}<Ph svg={unknownIcon} size={14} />{/if}
      <span class="title">{title}</span>
      {#if !known}<code class="raw">{cond.type ?? cond.kind}</code>{/if}
      {#each needs(def) as p (p)}<span class="perm">{p}</span>{/each}
    {/if}
    <span class="tools">
      {#if !isRoot}
        <button type="button" class="tool" disabled={index === 0} aria-label={$t('scripts.blocks.moveUp')} title={$t('scripts.blocks.moveUp')} on:click={() => ed.apply(shiftCond($tree, cp, -1))}><Ph svg={upIcon} size={13} /></button>
        <button type="button" class="tool" disabled={index >= siblings - 1} aria-label={$t('scripts.blocks.moveDown')} title={$t('scripts.blocks.moveDown')} on:click={() => ed.apply(shiftCond($tree, cp, 1))}><Ph svg={downIcon} size={13} /></button>
      {/if}
      {#if !group || !isRoot}
        <button type="button" class="tool" aria-label={$t('scripts.blocks.cond.wrap')} title={$t('scripts.blocks.cond.wrap')} on:click={() => ed.apply(wrapCond($tree, cp, 'all'))}><Ph svg={groupIcon} size={13} /></button>
      {/if}
      {#if cond.kind !== 'not'}
        <button type="button" class="tool" aria-label={$t('scripts.blocks.cond.negate')} title={$t('scripts.blocks.cond.negate')} on:click={() => ed.apply(wrapCond($tree, cp, 'not'))}><Ph svg={notIcon} size={13} /></button>
      {/if}
      {#if group && items.length === 1}
        <button type="button" class="tool text" on:click={() => ed.apply(unwrapCond($tree, cp))}>{$t('scripts.blocks.cond.ungroup')}</button>
      {/if}
      {#if testable}
        <button type="button" class="tool" aria-label={$t('scripts.match.try')} title={$t('scripts.match.try')} on:click={() => (testing = true)}><Ph svg={testIcon} size={13} /></button>
      {/if}
      {#if !isRoot || !group || items.length > 0}
        <button type="button" class="tool danger" aria-label={$t('scripts.blocks.cond.remove')} title={$t('scripts.blocks.cond.remove')} on:click={() => ed.apply(removeCond($tree, cp))}><Ph svg={trashIcon} size={13} /></button>
      {/if}
    </span>
  </div>

  {#if group}
    <ul class="items" class:drop-target={drop >= 0 && items.length === 0} data-cond-group={key} aria-label={title}>
      {#each items as item, i (i)}
        <li class="item" data-cond-index={i} class:drop-before={drop === i} class:drop-after={drop === items.length && i === items.length - 1}>
          <svelte:self cond={item} cp={{ ...cp, path: [...cp.path, i] }} {scope} siblings={items.length} />
        </li>
      {/each}
    </ul>
    {#if groupIssue}<p class="problem">{$t(`scripts.blocks.issue.${groupIssue.code}`)}</p>{/if}
    {#if cond.kind !== 'not' || items.length === 0}
      <button
        type="button"
        class="add"
        bind:this={addButton}
        on:click={() => ed.openInsert(addButton, { kind: 'cond', group: cp, index: items.length })}
      >
        <Ph svg={plusIcon} size={13} />{$t('scripts.blocks.addCondition')}
      </button>
    {/if}
  {:else if def}
    {#if def.params.length > 0}
      <div class="fields">
        {#each def.params as p (p.name)}
          <ArgField param={p} arg={cond.args?.[p.name]} {scope} problem={paramProblem(p.name)} idBase={`c-${key}`} on:change={(e) => setArg(p.name, e.detail)} />
        {/each}
      </div>
    {/if}
  {:else}
    <p class="problem">{$t('scripts.blocks.unknownKept')}</p>
  {/if}
</div>

{#if testing}
  <Modal title={$t('scripts.match.title')} size="large" on:close={() => (testing = false)}>
    <MatchTester when={onTestedMessage(cond, $catalog)} idBase={`m-${key}`} />
  </Modal>
{/if}

<style>
  .cond {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-2);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-base);
  }

  .cond.group {
    border-style: dashed;
    background: transparent;
  }

  .cond.root {
    padding: 0;
    border: none;
  }

  .cond.unknown {
    border-color: var(--warning);
    background: var(--warning-bg);
  }

  .cond.dragging {
    opacity: 0.4;
  }

  .head {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--space-2);
    min-height: var(--chip-height);
  }

  .title {
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
  }

  .raw,
  .perm {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .grip {
    display: inline-flex;
    color: var(--text-tertiary);
    cursor: grab;
    touch-action: none;
  }

  .tools {
    display: inline-flex;
    gap: var(--space-1);
    margin-inline-start: auto;
    opacity: 0;
  }

  .cond:hover > .head > .tools,
  .cond:focus-within > .head > .tools {
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
    min-width: var(--chip-height);
    height: var(--chip-height);
    padding: 0 var(--space-1);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
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

  .tool:focus-visible,
  .add:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .items {
    margin: 0;
    padding: 0 0 0 var(--space-3);
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    min-height: var(--space-3);
    border-inline-start: 2px solid var(--border-subtle);
    border-radius: var(--radius-control);
  }

  .items.drop-target {
    border: var(--hairline) dashed var(--accent);
    background: color-mix(in srgb, var(--accent) 4%, transparent);
  }

  .item {
    position: relative;
  }

  .item.drop-before::before,
  .item.drop-after::after {
    content: '';
    position: absolute;
    inset-inline: 0;
    height: calc(var(--hairline) * 2);
    background: var(--accent);
  }

  .item.drop-before::before {
    top: calc(-1 * var(--space-1));
  }

  .item.drop-after::after {
    bottom: calc(-1 * var(--space-1));
  }

  .fields {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(calc(var(--control-height) * 5), 1fr));
    gap: var(--space-2) var(--space-3);
  }

  .add {
    display: inline-flex;
    align-items: center;
    align-self: flex-start;
    gap: var(--space-1);
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
  }

  .add:hover {
    background: var(--surface-hover);
    color: var(--accent);
  }

  .problem {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--danger);
  }

  .cond.unknown .problem {
    color: var(--text-secondary);
  }
</style>
