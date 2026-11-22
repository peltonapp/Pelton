<script lang="ts">
  // the end of a guided import: the totals, then each part of the source
  // with what came over, what was left out and why.
  import { reportTotals, type ImportJob, type ImportReportLine } from '../../lib/guidedimport'
  import { t } from '../../lib/i18n'

  /** The finished, cancelled or failed job. */
  export let job: ImportJob

  $: lines = job.report.lines ?? []
  $: totals = reportTotals(lines)
  $: kinds = ['account', 'folder', 'contacts', 'calendar', 'filter'].filter((k) => lines.some((l) => l.kind === k))

  function reasons(line: ImportReportLine): string[] {
    return Object.entries(line.reasons ?? {}).map(([code, n]) =>
      $t(`import.guided.reason.${code}`).replace('{n}', n.toLocaleString()),
    )
  }
</script>

<section class="report" aria-labelledby="import-report-title">
  <h3 id="import-report-title" class:failed={job.state === 'failed'}>
    {$t(`import.guided.state.${job.state}`)}
  </h3>
  <p class="totals">
    {$t('import.guided.totals')
      .replace('{imported}', totals.imported.toLocaleString())
      .replace('{skipped}', totals.skipped.toLocaleString())
      .replace('{failed}', totals.failed.toLocaleString())}
  </p>
  {#if job.error}
    <code class="raw">{job.error}</code>
  {/if}

  {#each kinds as kind (kind)}
    <h4>{$t(`import.guided.kind.${kind}`)}</h4>
    <table>
      <thead>
        <tr>
          <th scope="col">{$t('import.guided.col.name')}</th>
          <th scope="col" class="num">{$t('import.guided.col.imported')}</th>
          <th scope="col" class="num">{$t('import.guided.col.skipped')}</th>
          <th scope="col" class="num">{$t('import.guided.col.failed')}</th>
        </tr>
      </thead>
      <tbody>
        {#each lines.filter((l) => l.kind === kind) as line (line.kind + line.name)}
          <tr>
            <th scope="row">
              <span class="name">{line.name}</span>
              {#each reasons(line) as r}
                <span class="why">{r}</span>
              {/each}
              {#each line.details ?? [] as d}
                <span class="why">{d}</span>
              {/each}
            </th>
            <td class="num">{line.imported.toLocaleString()}</td>
            <td class="num">{line.skipped.toLocaleString()}</td>
            <td class="num" class:bad={line.failed > 0}>{line.failed.toLocaleString()}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/each}
</section>

<style>
  .report {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  h3 {
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  h3.failed {
    color: var(--danger);
  }

  h4 {
    margin: var(--space-3) 0 0;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-secondary);
  }

  .totals {
    margin: 0;
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--fz-label);
  }

  th,
  td {
    padding: var(--space-1) var(--space-2);
    border-bottom: var(--hairline) solid var(--border-subtle);
    text-align: start;
    vertical-align: top;
    font-weight: var(--fw-regular);
    color: var(--text-primary);
  }

  thead th {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  tbody th {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .num {
    text-align: end;
    font-family: var(--font-mono);
    white-space: nowrap;
  }

  .bad {
    color: var(--danger);
  }

  .name {
    overflow-wrap: anywhere;
  }

  .why {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    overflow-wrap: anywhere;
  }

  .raw {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
    overflow-wrap: anywhere;
  }
</style>
