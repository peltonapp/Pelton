<script lang="ts">
  // the right pane of the Automations space: the runs of the selected script,
  // or of every script, newest first. each shows when, what started it, how it
  // ended, what it changed and how long it took; a run opens to its steps, and
  // one whose mail changes can still be undone offers that.
  import { onMount } from 'svelte'
  import logIcon from '@phosphor-icons/core/assets/duotone/list-bullets-duotone.svg?raw'
  import EmptyState from '../common/EmptyState.svelte'
  import RunSteps from './RunSteps.svelte'
  import { t } from '../../lib/i18n'
  import { formatRelative } from '../../lib/format'
  import { entryLabel, type ScriptRun } from '../../lib/scripting'
  import { clearScriptRuns, scriptLogDays, setScriptLogDays, undoScriptRun } from '../../lib/api'
  import { catalog, errorPlace, loadRuns, runLog, selectedScriptId } from '../../stores/scripts'
  import { errorMessage, toastError, toastInfo } from '../../stores/toast'
  import '../../theme/controls.css'

  let open: number | null = null
  let days = 30
  const undone = new Set<number>()

  onMount(async () => {
    try {
      days = await scriptLogDays()
    } catch {
      days = 30
    }
  })

  function triggerName(id: string): string {
    const def = $catalog?.triggers.find((tr) => tr.id === id)
    return def ? entryLabel($t, def) : id
  }

  async function clear(): Promise<void> {
    try {
      await clearScriptRuns($selectedScriptId ?? 0)
      await loadRuns()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function saveDays(value: string): Promise<void> {
    const n = Math.round(Number(value))
    if (!Number.isFinite(n) || n < 1 || n > 365) {
      return
    }
    try {
      await setScriptLogDays(n)
      days = n
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function undo(run: ScriptRun): Promise<void> {
    try {
      await undoScriptRun(run.id)
      undone.add(run.id)
      runLog.update((list) => [...list])
      toastInfo($t('scripts.log.undone'))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<section class="log" aria-label={$t('scripts.log.title')}>
  <header class="bar">
    <h2 class="title">{$t('scripts.log.title')}</h2>
    {#if $runLog.length > 0}
      <button type="button" class="cal-btn" on:click={() => void clear()}>{$t('scripts.log.clear')}</button>
    {/if}
  </header>
  {#if $runLog.length === 0}
    <EmptyState icon={logIcon} size="compact" title={$t('scripts.log.empty')} detail={$t('scripts.log.emptyDetail')} />
  {:else}
    <ul class="runs">
      {#each $runLog as run (run.id)}
        <li class="run {run.status}">
          <button type="button" class="head" aria-expanded={open === run.id} on:click={() => (open = open === run.id ? null : run.id)}>
            <span class="status">{$t(`scripts.status.${run.status}`)}</span>
            <span class="name">{$selectedScriptId === null ? run.scriptName : triggerName(run.trigger)}</span>
            <span class="when">{run.started ? formatRelative(Date.parse(run.started), $t) : ''}</span>
          </button>
          <p class="facts">
            {$t('scripts.log.facts').replace('{n}', String(run.changes)).replace('{ms}', String(run.durationMs))}
            {#if run.depth > 0}{$t('scripts.log.depth').replace('{n}', String(run.depth))}{/if}
          </p>
          {#if run.error}<p class="error">{#if errorPlace(run, $t)}<span class="where">{errorPlace(run, $t)}</span>{/if}{run.error}</p>{/if}
          {#if open === run.id}
            <RunSteps steps={run.steps} />
            {#if run.undoable && !undone.has(run.id)}
              <button type="button" class="cal-btn undo" on:click={() => void undo(run)}>{$t('scripts.log.undo')}</button>
            {/if}
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
  <footer class="foot">
    <label for="script-log-days">{$t('scripts.log.keep')}</label>
    <input id="script-log-days" class="cal-field days" type="number" min="1" max="365" value={days} on:change={(e) => void saveDays(e.currentTarget.value)} />
  </footer>
</section>

<style>
  .log {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    border-inline-start: var(--hairline) solid var(--border-default);
    background: var(--surface-base);
  }

  .bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    min-height: calc(var(--control-height) + 2 * var(--space-3));
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .title {
    margin: 0;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
  }

  .runs {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .run {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--row-pad-y) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .head {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    gap: var(--space-2);
    align-items: baseline;
    padding: 0;
    border: none;
    background: none;
    color: inherit;
    font: inherit;
    font-size: var(--fz-label);
    text-align: start;
    cursor: pointer;
  }

  .head:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .status {
    color: var(--success);
  }

  .run.error .status,
  .run.denied .status,
  .run.timeout .status,
  .run.limit .status {
    color: var(--danger);
  }

  .run.skipped .status,
  .run.stopped .status {
    color: var(--text-tertiary);
  }

  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .when,
  .facts {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .facts,
  .error {
    margin: 0;
  }

  .error {
    font-size: var(--fz-meta);
    color: var(--danger);
    overflow-wrap: anywhere;
  }

  .where {
    margin-inline-end: var(--space-1);
    font-family: var(--font-mono);
    font-weight: var(--fw-semibold);
  }

  .undo {
    align-self: flex-start;
  }

  .foot {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-4);
    border-top: var(--hairline) solid var(--border-subtle);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .days {
    width: calc(var(--control-height) * 2.5);
  }
</style>
