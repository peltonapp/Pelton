<script lang="ts">
  // the split inbox editor: which splits the inbox shows and in what order.
  // Shown splits move up and down and come off the list; the built-in ones not
  // shown and every saved view can be added. Other is always last.
  import upIcon from '@phosphor-icons/core/assets/duotone/caret-up-duotone.svg?raw'
  import downIcon from '@phosphor-icons/core/assets/duotone/caret-down-duotone.svg?raw'
  import plusIcon from '@phosphor-icons/core/assets/duotone/plus-duotone.svg?raw'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { t } from '../../lib/i18n'
  import { prefs, setSplitTabs } from '../../stores/prefs'
  import { views } from '../../stores/views'
  import { builtinSplits, otherSplit, splitKeys, splitViewId, viewSplitKey } from '../../stores/splits'

  $: shown = splitKeys($prefs.splitTabs, $views).filter((key) => key !== otherSplit)
  $: available = [
    ...builtinSplits.filter((key) => !shown.includes(key)),
    ...$views.map((v) => viewSplitKey(v.id)).filter((key) => !shown.includes(key)),
  ]

  /** The name a split shows under. */
  function nameOf(key: string): string {
    const id = splitViewId(key)
    if (id !== null) {
      return $views.find((v) => v.id === id)?.name ?? key
    }
    return $t(`split.tab.${key}`)
  }

  function save(keys: string[]): void {
    setSplitTabs(keys.join(','))
  }

  function move(index: number, by: -1 | 1): void {
    const next = [...shown]
    const [key] = next.splice(index, 1)
    next.splice(index + by, 0, key)
    save(next)
  }

  function remove(key: string): void {
    save(shown.filter((k) => k !== key))
  }

  function add(key: string): void {
    save([...shown, key])
  }
</script>

<div class="editor">
  <p class="hint">{$t('split.editor.hint')}</p>
  <span class="label">{$t('split.editor.shown')}</span>
  <ol class="list">
    {#each shown as key, i (key)}
      {@const name = nameOf(key)}
      <li class="item">
        <span class="name">{name}</span>
        <button
          type="button"
          class="tool"
          aria-label={$t('split.editor.moveUp').replace('{name}', name)}
          title={$t('split.editor.moveUp').replace('{name}', name)}
          disabled={i === 0}
          on:click={() => move(i, -1)}
        >
          <Ph svg={upIcon} size={13} />
        </button>
        <button
          type="button"
          class="tool"
          aria-label={$t('split.editor.moveDown').replace('{name}', name)}
          title={$t('split.editor.moveDown').replace('{name}', name)}
          disabled={i === shown.length - 1}
          on:click={() => move(i, 1)}
        >
          <Ph svg={downIcon} size={13} />
        </button>
        <button
          type="button"
          class="tool"
          aria-label={$t('split.editor.remove').replace('{name}', name)}
          title={$t('split.editor.remove').replace('{name}', name)}
          on:click={() => remove(key)}
        >
          <Ph svg={xIcon} size={13} />
        </button>
      </li>
    {/each}
    <li class="item fixed">
      <span class="name">{$t('split.tab.other')}</span>
      <span class="note">{$t('split.editor.otherLast')}</span>
    </li>
  </ol>
  {#if available.length > 0}
    <span class="label">{$t('split.editor.available')}</span>
    <ul class="list">
      {#each available as key (key)}
        {@const name = nameOf(key)}
        <li>
          <button type="button" class="add" aria-label={$t('split.editor.add').replace('{name}', name)} on:click={() => add(key)}>
            <Ph svg={plusIcon} size={13} />
            <span class="name">{name}</span>
          </button>
        </li>
      {/each}
    </ul>
  {/if}
  {#if $views.length === 0}
    <p class="hint">{$t('split.editor.noViews')}</p>
  {/if}
</div>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .hint {
    margin: 0 0 var(--space-2);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    line-height: 1.4;
  }

  .label {
    padding: var(--space-2) 0 var(--space-1);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    color: var(--text-tertiary);
  }

  .list {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    overflow: hidden;
  }

  .list > li + li {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .item {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    min-height: var(--control-height);
    padding: 0 var(--space-1) 0 var(--space-3);
    background: var(--surface-raised);
  }

  .item.fixed {
    background: var(--surface-sunken);
  }

  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .note {
    padding-inline-end: var(--space-2);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .tool {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .tool:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .tool:disabled {
    cursor: default;
    opacity: 0.35;
  }

  .add {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    min-height: var(--control-height);
    padding: 0 var(--space-3);
    border: none;
    background: var(--surface-raised);
    color: var(--text-secondary);
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .add:hover {
    background: var(--surface-hover);
  }

  .add .name {
    color: var(--text-secondary);
  }
</style>
