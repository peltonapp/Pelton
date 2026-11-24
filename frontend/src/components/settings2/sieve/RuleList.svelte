<script lang="ts">
  // the server filter rules in the order the server runs them: each can be
  // switched off, opened for editing, duplicated, deleted or dragged to a new
  // place. edits stay here until Save, which hands the whole list to the page.
  import { createEventDispatcher } from 'svelte'
  import { slide } from 'svelte/transition'
  import gripIcon from '@phosphor-icons/core/assets/duotone/dots-six-vertical-duotone.svg?raw'
  import caretIcon from '@phosphor-icons/core/assets/duotone/caret-right-duotone.svg?raw'
  import copyIcon from '@phosphor-icons/core/assets/duotone/copy-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import plusIcon from '@phosphor-icons/core/assets/duotone/plus-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import PieProgress from '../../common/PieProgress.svelte'
  import RuleEditor from './RuleEditor.svelte'
  import { reorder, reorderHandleAttr, type ReorderDetail } from '../../../lib/reorder'
  import { actionMenuKind, blankRule, copyRule, operatorKey, ruleProblem, sameModel } from '../../../lib/sieve'
  import { dur, motion } from '../../../lib/motion'
  import { askConfirm } from '../../../stores/confirm'
  import { t } from '../../../lib/i18n'
  import type { SelectOption } from '../../../lib/selectnav'
  import type { SieveCaps, SieveRule } from '../../../lib/types'

  /** The rules as they are on the server. */
  export let saved: SieveRule[]
  /** What the server supports. */
  export let caps: SieveCaps
  /** The account's folders, valued by their server path. */
  export let folders: SelectOption[] = []
  /** True while a save is running. */
  export let busy: boolean = false
  /** The server's objection to the last save, '' for none. */
  export let problem: string = ''

  const dispatch = createEventDispatcher<{ save: SieveRule[] }>()

  interface Item {
    key: number
    rule: SieveRule
  }

  let nextKey = 1
  let items: Item[] = []
  let open = new Set<number>()
  let lastSaved: SieveRule[] | undefined = undefined
  $: if (saved !== lastSaved) {
    lastSaved = saved
    reset()
  }

  function reset(): void {
    items = saved.map((rule) => ({ key: nextKey++, rule: copyRule(rule, rule.name) }))
    open = new Set()
  }

  $: rules = items.map((i) => i.rule)
  $: dirty = !sameModel(rules, saved)
  $: firstProblem = items.find((i) => ruleProblem(i.rule) !== '')
  $: invalid = firstProblem
    ? $t('sieve.problem.inRule')
        .replace('{name}', firstProblem.rule.name || $t('sieve.rule.unnamed'))
        .replace('{problem}', $t(ruleProblem(firstProblem.rule)))
    : ''

  function setRule(key: number, rule: SieveRule): void {
    items = items.map((i) => (i.key === key ? { ...i, rule } : i))
  }

  function toggleOpen(key: number): void {
    const next = new Set(open)
    if (next.has(key)) {
      next.delete(key)
    } else {
      next.add(key)
    }
    open = next
  }

  function add(): void {
    const key = nextKey++
    items = [...items, { key, rule: blankRule($t('sieve.filters.newName').replace('{n}', String(items.length + 1))) }]
    open = new Set([...open, key])
  }

  function duplicate(item: Item): void {
    const key = nextKey++
    const at = items.indexOf(item)
    const copy = copyRule(item.rule, $t('sieve.filters.copyName').replace('{name}', item.rule.name))
    items = [...items.slice(0, at + 1), { key, rule: copy }, ...items.slice(at + 1)]
  }

  async function remove(item: Item): Promise<void> {
    const ok = await askConfirm({
      title: $t('sieve.rule.deleteConfirm').replace('{name}', item.rule.name || $t('sieve.rule.unnamed')),
      body: $t('sieve.rule.deleteBody'),
      confirmLabel: $t('sieve.rule.delete'),
      danger: true,
    })
    if (ok) {
      items = items.filter((i) => i !== item)
    }
  }

  function onReorder(event: CustomEvent<ReorderDetail>): void {
    const byKey = new Map(items.map((i) => [String(i.key), i]))
    items = event.detail.ids.map((id) => byKey.get(id)).filter((i): i is Item => i !== undefined)
  }

  // alt with an arrow on the grip moves a rule without a pointer.
  function onGripKeydown(event: KeyboardEvent, item: Item): void {
    if (!event.altKey || (event.key !== 'ArrowUp' && event.key !== 'ArrowDown')) {
      return
    }
    event.preventDefault()
    const at = items.indexOf(item)
    const to = at + (event.key === 'ArrowUp' ? -1 : 1)
    if (to < 0 || to >= items.length) {
      return
    }
    const next = [...items]
    next.splice(at, 1)
    next.splice(to, 0, item)
    items = next
  }

  function summary(rule: SieveRule): string {
    const conds = rule.conditions.map((c) => {
      const field = c.field === 'header' ? c.header || $t('sieve.field.header') : $t(`sieve.field.${c.field}`)
      const op = $t(`sieve.op.${operatorKey(c)}`)
      return c.op === 'exists' ? `${field} ${op}` : `${field} ${op} ${c.value}`
    })
    const when = conds.length === 0 ? $t('sieve.rule.always') : conds.join(rule.matchAny ? ` ${$t('sieve.rule.or')} ` : ` ${$t('sieve.rule.and')} `)
    const then = rule.actions
      .map((a) => {
        const label = $t(`sieve.action.${actionMenuKind(a)}`)
        return a.kind === 'fileinto' || a.kind === 'redirect' ? `${label} ${a.arg}` : label
      })
      .join(', ')
    return `${when}: ${then}`
  }
</script>

{#if items.length === 0}
  <p class="empty">{$t('sieve.filters.empty')}</p>
{/if}
<div class="list" use:reorder on:reorder={onReorder}>
  {#each items as item (item.key)}
    <div class="rule" class:off={!item.rule.enabled} data-reorder-id={String(item.key)}>
      <div class="head">
        <button
          type="button"
          class="grip"
          title={$t('sieve.rule.drag')}
          aria-label={$t('sieve.rule.drag')}
          {...{ [reorderHandleAttr]: '' }}
          on:keydown={(e) => onGripKeydown(e, item)}
        >
          <Ph svg={gripIcon} size={14} />
        </button>
        <ToggleSwitch
          checked={item.rule.enabled}
          label={$t('sieve.rule.enabled')}
          on:change={(e) => setRule(item.key, { ...item.rule, enabled: e.detail })}
        />
        <button
          type="button"
          class="title"
          aria-expanded={open.has(item.key)}
          on:click={() => toggleOpen(item.key)}
        >
          <span class="caret" class:turned={open.has(item.key)}><Ph svg={caretIcon} size={12} /></span>
          <span class="name">{item.rule.name || $t('sieve.rule.unnamed')}</span>
          <span class="summary">{summary(item.rule)}</span>
        </button>
        <button type="button" class="icon" title={$t('sieve.rule.duplicate')} aria-label={$t('sieve.rule.duplicate')} on:click={() => duplicate(item)}>
          <Ph svg={copyIcon} size={15} />
        </button>
        <button type="button" class="icon" title={$t('sieve.rule.delete')} aria-label={$t('sieve.rule.delete')} on:click={() => remove(item)}>
          <Ph svg={trashIcon} size={15} />
        </button>
      </div>
      {#if open.has(item.key)}
        <div transition:slide|local={{ duration: dur(motion.base) }}>
          <RuleEditor rule={item.rule} {caps} {folders} on:change={(e) => setRule(item.key, e.detail)} />
        </div>
      {/if}
    </div>
  {/each}
</div>
<button type="button" class="s2-btn add" on:click={add}>
  <Ph svg={plusIcon} size={14} />
  {$t('sieve.filters.add')}
</button>

<div class="sv-bar">
  {#if problem}
    <p class="sv-problem">{problem}</p>
  {:else if dirty && invalid}
    <p class="sv-problem">{invalid}</p>
  {/if}
  {#if busy}
    <PieProgress label={$t('sieve.saving')} />
  {/if}
  {#if dirty}
    <button type="button" class="s2-btn quiet" disabled={busy} on:click={reset}>{$t('sieve.discard')}</button>
  {/if}
  <button type="button" class="s2-btn primary" disabled={busy || !dirty || invalid !== ''} on:click={() => dispatch('save', rules)}>
    {$t('sieve.save')}
  </button>
</div>

<style>
  .empty {
    margin: 0 0 var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .list {
    display: flex;
    flex-direction: column;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  .list:empty {
    display: none;
  }

  .rule + .rule {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: calc(var(--control-height) + 2 * var(--space-2));
    padding: var(--space-1) var(--space-3) var(--space-1) var(--space-1);
  }

  .grip,
  .icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: var(--control-height);
    height: var(--control-height);
    padding: 0;
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
  }

  .grip {
    width: calc(var(--control-height) * 0.7);
    cursor: grab;
  }

  .grip:active {
    cursor: grabbing;
  }

  .icon:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .title {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    text-align: start;
    cursor: var(--cursor-action);
  }

  .title:hover {
    background: var(--surface-hover);
  }

  .caret {
    display: inline-flex;
    align-self: center;
    color: var(--text-tertiary);
    transition: transform var(--motion-fast) var(--ease-out);
  }

  .caret.turned {
    transform: rotate(90deg);
  }

  :global(:root[data-reduce-motion]) .caret {
    transition: none;
  }

  .name {
    flex-shrink: 0;
    max-width: 40%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-list);
  }

  .summary {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .rule.off .name,
  .rule.off .summary {
    color: var(--text-tertiary);
  }

  .add {
    margin-top: var(--space-3);
  }

  .sv-bar {
    margin-top: var(--space-4);
  }

  @media (prefers-reduced-motion: reduce) {
    .caret {
      transition: none;
    }
  }
</style>
