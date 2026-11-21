<script lang="ts">
  // editable keyboard shortcuts. each app action shows its current combo and can
  // be rebound by clicking Change and pressing the new keys; a search box filters
  // the list, and each row (and the whole set) can be reset to the default.
  // capturing is exact: the recorded combo includes every held modifier.
  import { onDestroy } from 'svelte'
  import { get } from 'svelte/store'
  import { IconSearch, IconX, IconRotateClockwise } from '@tabler/icons-svelte'
  import { shortcuts as registry, eventToCombo, type ShortcutAction } from '../../lib/shortcuts'
  import { bindings, recording, setBinding, resetBinding, resetAll, conflictsFor } from '../../stores/shortcuts'
  import { t, shortcutLabel } from '../../lib/i18n'

  let query = ''

  // the action currently being recorded, plus any conflict message to show.
  let recordingAction: ShortcutAction | null = null
  let conflictMsg = ''

  // filter rows by their localized label (and the action key as a fallback).
  // $t is a dependency so this re-filters when the language changes too.
  $: rows = registry.filter((s) => {
    const q = query.trim().toLowerCase()
    if (!q) {
      return true
    }
    return $t(s.labelKey).toLowerCase().includes(q) || s.action.includes(q)
  })

  function startRecord(action: ShortcutAction): void {
    recordingAction = action
    conflictMsg = ''
    recording.set(true)
  }

  function stopRecord(): void {
    recordingAction = null
    conflictMsg = ''
    recording.set(false)
  }

  // leaving the section mid-recording must hand the keyboard back, or no
  // shortcut works until Settings is opened again.
  onDestroy(stopRecord)

  // onKey captures the new combo. Escape cancels; a bare modifier is ignored so
  // the user can build a combo; a duplicate is rejected with an inline message.
  function onKey(event: KeyboardEvent): void {
    if (recordingAction === null) {
      return
    }
    event.preventDefault()
    event.stopPropagation()
    if (event.key === 'Escape') {
      stopRecord()
      return
    }
    const combo = eventToCombo(event)
    if (!combo) {
      return
    }
    const clash = conflictsFor(recordingAction, combo)
    if (clash) {
      conflictMsg = `${get(t)('shortcuts.alreadyUsedBy')} “${get(t)(registryLabel(clash))}”`
      return
    }
    setBinding(recordingAction, combo)
    stopRecord()
  }

  function registryLabel(action: ShortcutAction): string {
    return registry.find((s) => s.action === action)?.labelKey ?? action
  }
</script>

<svelte:window on:keydown={onKey} />

<div class="head-row">
  <div class="search">
    <IconSearch size={15} stroke={1.6} />
    <input type="search" placeholder={$t('shortcuts.searchPlaceholder')} aria-label={$t('shortcuts.searchPlaceholder')} bind:value={query} />
    {#if query}
      <button type="button" class="clear" aria-label={$t('shortcuts.clear')} on:click={() => (query = '')}>
        <IconX size={14} stroke={1.8} />
      </button>
    {/if}
  </div>
  <button type="button" class="reset-all" on:click={resetAll} title={$t('shortcuts.resetAllTitle')}>
    <IconRotateClockwise size={14} stroke={1.7} /> {$t('shortcuts.resetAll')}
  </button>
</div>

<ul class="list">
  {#each rows as sc (sc.action)}
    <li>
      <span class="label">{$t(sc.labelKey)}</span>
      <div class="controls">
        {#if recordingAction === sc.action}
          <span class="recording">{$t('shortcuts.pressKeys')} <kbd>Esc</kbd> {$t('shortcuts.toCancel')}</span>
        {:else if $bindings[sc.action]}
          <kbd>{shortcutLabel($bindings[sc.action])}</kbd>
          <!-- an action with an alternate answers to both keys until it is
               rebound, so listing only one would be a half truth. -->
          {#if sc.alt && $bindings[sc.action] === sc.combo}
            <span class="alt-sep">{$t('shortcuts.or')}</span>
            <kbd>{shortcutLabel(sc.alt)}</kbd>
          {/if}
        {:else}
          <span class="unset">{$t('shortcuts.notSet')}</span>
        {/if}
        <button type="button" class="change" on:click={() => startRecord(sc.action)}>
          {recordingAction === sc.action ? $t('shortcuts.recording') : $t('shortcuts.change')}
        </button>
        <button
          type="button"
          class="reset"
          aria-label={$t('shortcuts.resetToDefault')}
          title={$t('shortcuts.resetToDefault')}
          on:click={() => resetBinding(sc.action)}
        >
          <IconRotateClockwise size={14} stroke={1.7} />
        </button>
      </div>
    </li>
  {/each}
  {#if rows.length === 0}
    <li class="empty">{$t('shortcuts.noMatch')} “{query}”.</li>
  {/if}
</ul>

{#if conflictMsg}
  <p class="conflict">{conflictMsg}. {$t('shortcuts.pickDifferent')}</p>
{/if}

<style>
  .head-row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin-bottom: var(--space-3);
  }

  .search {
    flex: 1;
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: 0 var(--space-3);
    height: var(--control-height);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-tertiary);
  }

  .search:focus-within {
    border-color: var(--accent);
  }

  .search input {
    flex: 1;
    min-width: 0;
    border: none;
    background: transparent;
    outline: none;
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .search input::-webkit-search-cancel-button {
    display: none;
  }

  .clear {
    display: inline-flex;
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    padding: 2px;
  }

  .reset-all {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    flex-shrink: 0;
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-secondary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .reset-all:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-2) 0;
  }

  .empty {
    color: var(--text-tertiary);
    font-size: var(--fz-label);
    justify-content: flex-start;
  }

  .label {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .controls {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }

  kbd {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-primary);
    background: var(--surface-sunken);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    padding: 2px var(--space-2);
  }

  .alt-sep {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .recording {
    font-size: var(--fz-meta);
    color: var(--accent);
  }

  .recording kbd {
    color: var(--text-secondary);
  }

  .change,
  .reset {
    display: inline-flex;
    align-items: center;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    font-size: var(--fz-meta);
  }

  .change {
    padding: 2px var(--space-3);
  }

  .reset {
    padding: 2px;
  }

  .change:hover,
  .reset:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .conflict {
    margin: var(--space-2) 0 0;
    font-size: var(--fz-label);
    color: var(--warning);
  }
</style>
