<script lang="ts">
  // the body of one server filter rule: its name, its conditions and how they
  // combine, and its actions. every edit is dispatched as a new rule; the list
  // that holds it decides when anything is saved.
  import { createEventDispatcher } from 'svelte'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import plusIcon from '@phosphor-icons/core/assets/duotone/plus-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import Select from '../../common/Select.svelte'
  import Segmented from '../ui/Segmented.svelte'
  import {
    actionFromMenu,
    actionKinds,
    actionMenuKind,
    blankCondition,
    conditionFields,
    needsValue,
    operatorKey,
    operatorKeys,
    ruleProblem,
    withField,
    withOperator,
  } from '../../../lib/sieve'
  import { t } from '../../../lib/i18n'
  import type { SelectOption } from '../../../lib/selectnav'
  import type { SieveAction, SieveCaps, SieveCondition, SieveRule } from '../../../lib/types'

  /** The rule being edited. */
  export let rule: SieveRule
  /** What the server supports, which decides the fields and actions offered. */
  export let caps: SieveCaps
  /** The account's folders, valued by their server path. */
  export let folders: SelectOption[] = []

  const dispatch = createEventDispatcher<{ change: SieveRule }>()

  function update(patch: Partial<SieveRule>): void {
    dispatch('change', { ...rule, ...patch })
  }

  function setCondition(index: number, next: SieveCondition): void {
    update({ conditions: rule.conditions.map((c, i) => (i === index ? next : c)) })
  }

  function setAction(index: number, next: SieveAction): void {
    update({ actions: rule.actions.map((a, i) => (i === index ? next : a)) })
  }

  function addAction(): void {
    const used = rule.actions.map(actionMenuKind)
    const kind = actionKinds(caps).find((k) => !used.includes(k)) ?? 'keep'
    update({ actions: [...rule.actions, actionFromMenu(kind)] })
  }

  $: fieldItems = conditionFields(caps).map((f) => ({ value: f, label: $t(`sieve.field.${f}`) }))
  $: kindItems = (current: string): SelectOption[] => {
    const kinds = actionKinds(caps)
    const list = kinds.includes(current) ? kinds : [current, ...kinds]
    return list.map((k) => ({ value: k, label: $t(`sieve.action.${k}`) }))
  }
  $: opItems = (field: string): SelectOption[] => operatorKeys(field).map((k) => ({ value: k, label: $t(`sieve.op.${k}`) }))
  $: folderItems = (current: string): SelectOption[] =>
    current && !folders.some((f) => f.value === current) ? [{ value: current, label: current }, ...folders] : folders
  $: matchOptions = [
    { key: 'all', label: $t('sieve.rule.matchAll') },
    { key: 'any', label: $t('sieve.rule.matchAny') },
  ]
  $: problem = ruleProblem(rule)
</script>

<div class="editor">
  <label class="name">
    <span class="caption">{$t('sieve.rule.name')}</span>
    <input class="sv-input" type="text" value={rule.name} on:input={(e) => update({ name: e.currentTarget.value })} />
  </label>

  <div class="section">
    <div class="section-head">
      <span class="caption">{$t('sieve.rule.match')}</span>
      {#if rule.conditions.length > 1}
        <Segmented
          label={$t('sieve.rule.match')}
          value={rule.matchAny ? 'any' : 'all'}
          options={matchOptions}
          on:change={(e) => update({ matchAny: e.detail === 'any' })}
        />
      {/if}
    </div>
    {#if rule.conditions.length === 0}
      <p class="sv-note">{$t('sieve.rule.noConditions')}</p>
    {/if}
    <ul class="lines">
      {#each rule.conditions as condition, i (i)}
        <li class="line" aria-label={$t('sieve.rule.condition').replace('{n}', String(i + 1))}>
          <Select
            value={condition.field}
            items={fieldItems}
            ariaLabel={$t('sieve.rule.field')}
            on:change={(e) => setCondition(i, withField(condition, e.detail))}
          />
          {#if condition.field === 'header'}
            <input
              class="sv-input header"
              type="text"
              placeholder={$t('sieve.headerName')}
              aria-label={$t('sieve.headerName')}
              spellcheck="false"
              value={condition.header}
              on:input={(e) => setCondition(i, { ...condition, header: e.currentTarget.value })}
            />
          {/if}
          <Select
            value={operatorKey(condition)}
            items={opItems(condition.field)}
            ariaLabel={$t('sieve.rule.operator')}
            on:change={(e) => setCondition(i, withOperator(condition, e.detail))}
          />
          {#if needsValue(condition)}
            <input
              class="sv-input value"
              type="text"
              aria-label={$t('sieve.value')}
              placeholder={condition.field === 'size' ? $t('sieve.sizePlaceholder') : ''}
              value={condition.value}
              on:input={(e) => setCondition(i, { ...condition, value: e.currentTarget.value })}
            />
          {/if}
          <button
            type="button"
            class="remove"
            aria-label={$t('sieve.rule.removeCondition')}
            title={$t('sieve.rule.removeCondition')}
            on:click={() => update({ conditions: rule.conditions.filter((_, j) => j !== i) })}
          >
            <Ph svg={xIcon} size={14} />
          </button>
        </li>
      {/each}
    </ul>
    <button type="button" class="s2-btn quiet add" on:click={() => update({ conditions: [...rule.conditions, blankCondition()] })}>
      <Ph svg={plusIcon} size={14} />
      {$t('sieve.rule.addCondition')}
    </button>
  </div>

  <div class="section">
    <div class="section-head">
      <span class="caption">{$t('sieve.rule.then')}</span>
    </div>
    <ul class="lines">
      {#each rule.actions as action, i (i)}
        <li class="line" aria-label={$t('sieve.rule.action').replace('{n}', String(i + 1))}>
          <Select
            value={actionMenuKind(action)}
            items={kindItems(actionMenuKind(action))}
            ariaLabel={$t('sieve.rule.actionKind')}
            on:change={(e) => setAction(i, actionFromMenu(e.detail))}
          />
          {#if action.kind === 'fileinto'}
            <Select
              value={action.arg}
              items={folderItems(action.arg)}
              ariaLabel={$t('sieve.action.folder')}
              on:change={(e) => setAction(i, { ...action, arg: e.detail })}
            />
          {:else if action.kind === 'redirect'}
            <input
              class="sv-input value"
              type="email"
              aria-label={$t('sieve.action.address')}
              placeholder={$t('sieve.action.address')}
              spellcheck="false"
              value={action.arg}
              on:input={(e) => setAction(i, { ...action, arg: e.currentTarget.value.trim() })}
            />
          {/if}
          <button
            type="button"
            class="remove"
            aria-label={$t('sieve.rule.removeAction')}
            title={$t('sieve.rule.removeAction')}
            on:click={() => update({ actions: rule.actions.filter((_, j) => j !== i) })}
          >
            <Ph svg={xIcon} size={14} />
          </button>
        </li>
      {/each}
    </ul>
    <button type="button" class="s2-btn quiet add" on:click={addAction}>
      <Ph svg={plusIcon} size={14} />
      {$t('sieve.rule.addAction')}
    </button>
  </div>

  {#if problem}
    <p class="sv-problem">{$t(problem)}</p>
  {/if}
</div>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    padding: var(--space-3) var(--space-4) var(--space-4);
  }

  .name {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    max-width: 28em;
  }

  .caption {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .section {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .section-head {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: var(--control-height);
  }

  .lines {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }

  .header {
    width: 10em;
  }

  .value {
    flex: 1 1 12em;
  }

  .remove {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--control-height);
    height: var(--control-height);
    padding: 0;
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
  }

  .remove:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .add {
    align-self: flex-start;
    padding: 0 var(--space-2);
  }
</style>
