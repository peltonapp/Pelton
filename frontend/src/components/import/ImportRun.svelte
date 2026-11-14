<script lang="ts" context="module">
  import type { ImportProgressEvent } from '../../lib/events'

  /** One mailbox or file of an import run. */
  export interface RunItem {
    key: string
    label: string
  }

  /** A mail import in progress or just finished, with what it was asked to read. */
  export interface Run {
    items: RunItem[]
    progress: ImportProgressEvent | null
    finished: boolean
  }
</script>

<script lang="ts">
  // the progress of a mail import: a bar for the whole run, a row with a pie
  // per mailbox or file, and the totals once it ends. the backend reports bytes
  // for the run and which source it is on, so a row is waiting, being read or
  // done; only the bar knows how far along the run is.
  import PieProgress from '../common/PieProgress.svelte'
  import Ph from '../common/Ph.svelte'
  import checkIcon from '@phosphor-icons/core/assets/duotone/check-circle-duotone.svg?raw'
  import warnIcon from '@phosphor-icons/core/assets/duotone/warning-circle-duotone.svg?raw'
  import { t } from '../../lib/i18n'

  export let run: Run

  $: progress = run.progress
  $: failed = run.finished && !!progress?.error
  $: current = Math.max(0, (progress?.fileIndex ?? 0) - 1)
  $: percent =
    run.finished && !failed
      ? 100
      : progress && progress.bytesTotal > 0
        ? Math.min(100, Math.round((progress.bytesDone / progress.bytesTotal) * 100))
        : 0

  // a row's pie: full once read, turning while read, empty while waiting.
  function rowValue(index: number): number | null {
    if (run.finished) {
      return failed && index >= current ? 0 : 1
    }
    if (!progress || progress.fileIndex === 0) {
      return 0
    }
    if (index < current) {
      return 1
    }
    return index === current ? null : 0
  }
</script>

<section class="run" aria-live="polite">
  <div class="head">
    <span class="where">
      {#if failed}
        {$t('import.run.failed')}
      {:else if run.finished}
        {$t('import.finished')}
      {:else if progress?.folder}
        {progress.fileTotal > 1
          ? $t('import.progress.mailboxOf')
              .replace('{name}', progress.folder)
              .replace('{i}', String(progress.fileIndex))
              .replace('{n}', String(progress.fileTotal))
          : progress.folder}
      {:else}
        {$t('import.importing')}
      {/if}
    </span>
    <span class="count">{percent}%</span>
  </div>
  <div
    class="bar"
    role="progressbar"
    aria-label={$t('import.importing')}
    aria-valuemin={0}
    aria-valuemax={100}
    aria-valuenow={percent}
  >
    <div class="fill" class:failed style:width="{percent}%"></div>
  </div>

  <ul class="items">
    {#each run.items as item, index (item.key)}
      {@const value = rowValue(index)}
      <li class:done={value === 1}>
        <span class="pie"><PieProgress {value} size={14} label={item.label} /></span>
        <span class="label">{item.label}</span>
      </li>
    {/each}
  </ul>

  {#if run.finished && progress}
    <div class="summary" class:failed>
      <span class="summary-icon"><Ph svg={failed ? warnIcon : checkIcon} size={18} /></span>
      <span class="summary-text">
        <span class="summary-main">{progress.imported.toLocaleString()} {$t('import.messagesImported')}</span>
        {#if progress.skipped > 0}
          <span>{$t('import.skipped')}: {progress.skipped.toLocaleString()}</span>
        {/if}
        {#if progress.failed > 0}
          <span>{$t('import.failed')}: {progress.failed.toLocaleString()}</span>
        {/if}
        {#if progress.error}
          <code class="raw">{progress.error}</code>
        {:else}
          <span class="where-to">{$t('import.run.where')}</span>
        {/if}
      </span>
    </div>
  {/if}
</section>

<style>
  .run {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding-top: var(--space-4);
    border-top: var(--hairline) solid var(--border-default);
  }

  .head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-3);
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .where {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .count {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .bar {
    height: var(--space-2);
    border-radius: var(--space-1);
    background: var(--surface-sunken);
    overflow: hidden;
  }

  .fill {
    height: 100%;
    background: var(--accent);
    transition: width var(--motion-base) var(--ease-out);
  }

  .fill.failed {
    background: var(--danger);
  }

  .items {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    max-height: 10rem;
    margin: 0;
    padding: 0;
    overflow-y: auto;
    list-style: none;
  }

  .items li {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .items li.done {
    color: var(--text-primary);
  }

  .pie {
    display: inline-flex;
    color: var(--accent);
  }

  .label {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .summary {
    display: flex;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border-inline-start: 2px solid var(--success);
    background: var(--success-bg);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .summary.failed {
    border-inline-start-color: var(--danger);
    background: var(--danger-bg);
  }

  .summary-icon {
    display: inline-flex;
    color: var(--success);
  }

  .summary.failed .summary-icon {
    color: var(--danger);
  }

  .summary-text {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .summary-main {
    font-weight: var(--fw-semibold);
  }

  .where-to {
    color: var(--text-secondary);
  }

  .raw {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
    overflow-wrap: anywhere;
  }
</style>
