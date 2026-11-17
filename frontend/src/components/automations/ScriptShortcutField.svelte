<script lang="ts">
  // the key a script placed as a shortcut answers to. It records and clears
  // through the same bindings store and conflict check as Settings > Shortcuts.
  import { onDestroy } from 'svelte'
  import { get } from 'svelte/store'
  import { eventToCombo, scriptAction, shortcuts as registry } from '../../lib/shortcuts'
  import { shortcutLabel, t } from '../../lib/i18n'
  import { bindings, conflictsFor, recording, scriptShortcuts, setBinding } from '../../stores/shortcuts'

  /** The script the key runs. */
  export let scriptId: number

  let capturing = false
  let conflict = ''

  $: action = scriptAction(scriptId)
  $: combo = $bindings[action] ?? ''

  function start(): void {
    capturing = true
    conflict = ''
    recording.set(true)
  }

  function stop(): void {
    capturing = false
    conflict = ''
    recording.set(false)
  }

  onDestroy(() => {
    if (capturing) {
      stop()
    }
  })

  function nameOf(other: string): string {
    const entry = [...registry, ...get(scriptShortcuts)].find((s) => s.action === other)
    return entry ? (entry.label ?? get(t)(entry.labelKey)) : other
  }

  function onKey(event: KeyboardEvent): void {
    if (!capturing) {
      return
    }
    event.preventDefault()
    event.stopPropagation()
    if (event.key === 'Escape') {
      stop()
      return
    }
    const next = eventToCombo(event)
    if (!next) {
      return
    }
    const clash = conflictsFor(action, next)
    if (clash) {
      conflict = `${get(t)('shortcuts.alreadyUsedBy')} “${nameOf(clash)}”`
      return
    }
    setBinding(action, next)
    stop()
  }

  function clear(): void {
    setBinding(action, '')
  }
</script>

<svelte:window on:keydown|capture={onKey} />

<div class="field">
  <div class="line">
    {#if capturing}
      <span class="recording">{$t('shortcuts.pressKeys')} <kbd>Esc</kbd> {$t('shortcuts.toCancel')}</span>
    {:else if combo}
      <kbd>{shortcutLabel(combo)}</kbd>
    {:else}
      <span class="unset">{$t('shortcuts.notSet')}</span>
    {/if}
    <button type="button" class="btn" on:click={capturing ? stop : start}>
      {capturing ? $t('shortcuts.recording') : $t('shortcuts.change')}
    </button>
    {#if combo && !capturing}
      <button type="button" class="btn" on:click={clear}>{$t('shortcuts.clear')}</button>
    {/if}
  </div>
  {#if conflict}
    <p class="conflict">{conflict}. {$t('shortcuts.pickDifferent')}</p>
  {/if}
</div>

<style>
  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .line {
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

  .unset {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .recording {
    font-size: var(--fz-meta);
    color: var(--accent);
  }

  .btn {
    padding: 2px var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
  }

  .btn:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .conflict {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--warning);
  }
</style>
