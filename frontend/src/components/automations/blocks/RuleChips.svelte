<script lang="ts">
  // the chips above a rule made from a message: one click matches it on the
  // sender, the subject, the List-Id or a recipient instead.
  import xIcon from '@phosphor-icons/core/assets/duotone/x-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import { t } from '../../../lib/i18n'
  import { getEditor } from './context'
  import { ruleDraft, type RuleChoice, type RuleDraft } from './rulefrommail'
  import { emptyGroup, findBlock, setCond } from './tree'

  /** The rule on screen. */
  export let draft: RuleDraft

  const ed = getEditor()
  const { tree } = ed

  $: when = findBlock($tree, draft.block)?.branches?.[0]?.when
  $: current = when?.items?.length === 1 ? JSON.stringify(when.items[0]) : ''

  function pick(c: RuleChoice): void {
    ed.apply(setCond($tree, { block: draft.block, branch: 0, path: [] }, { ...emptyGroup(), items: [c.cond] }), draft.block)
  }
</script>

<div class="rule" role="group" aria-label={$t('scripts.blocks.rule.matchOn')}>
  <span class="label">{$t('scripts.blocks.rule.matchOn')}</span>
  {#each draft.choices as c (c.key)}
    <button type="button" class="chip" class:on={current === JSON.stringify(c.cond)} aria-pressed={current === JSON.stringify(c.cond)} title={c.value} on:click={() => pick(c)}>
      <span class="key">{$t(`scripts.blocks.rule.${c.key}`)}</span>
      <span class="value">{c.value}</span>
    </button>
  {/each}
  <button type="button" class="close" aria-label={$t('scripts.blocks.rule.dismiss')} title={$t('scripts.blocks.rule.dismiss')} on:click={() => ruleDraft.set(null)}>
    <Ph svg={xIcon} size={12} />
  </button>
</div>

<style>
  .rule {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-2);
    border-inline-start: 2px solid var(--accent);
    background: var(--selection-bg);
    border-radius: var(--radius-control);
  }

  .label {
    margin-inline-end: var(--space-1);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .chip {
    display: inline-flex;
    align-items: baseline;
    gap: var(--space-1);
    max-width: calc(var(--control-height) * 8);
    min-height: var(--chip-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-base);
    color: var(--text-primary);
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
  }

  .chip.on {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--accent-fg);
  }

  .chip:focus-visible,
  .close:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .key {
    font-weight: var(--fw-semibold);
  }

  .value {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .close {
    display: inline-flex;
    margin-inline-start: auto;
    padding: var(--space-1);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
  }
</style>
