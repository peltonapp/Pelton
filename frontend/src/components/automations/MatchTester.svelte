<script lang="ts">
  // tries a regular expression, or a condition or group from a block script,
  // on the newest messages of a folder and lists which match, the matching
  // part marked. the backend runs it with the same RE2 engine and condition
  // code a script does, read only and within a time limit.
  import Select from '../common/Select.svelte'
  import Live from './Live.svelte'
  import { t } from '../../lib/i18n'
  import { testScriptMatch } from '../../lib/api'
  import { formatDateTimeMedium } from '../../lib/format'
  import type { BlockCond, MatchHit, MatchReport } from '../../lib/scripting'
  import { sidebar } from '../../stores/accounts'
  import { errorMessage } from '../../stores/toast'
  import { re2Problem } from './blocks/re2'
  import '../../theme/controls.css'

  /** A condition or group to test; without one the pattern fields show. */
  export let when: BlockCond | null = null
  /** The regular expression to start with. */
  export let pattern = ''
  /** The field the pattern is tried on: subject, from, to, body or headers.<name>. */
  export let field = 'subject'
  /** Prefix for element ids, unique on the page. */
  export let idBase = 'match'

  const roles = ['inbox', 'archive', 'sent', 'junk', 'trash', 'drafts']
  const limits = [50, 100, 200, 500]

  let folder = 'inbox'
  let limit = 100
  let header = field.startsWith('headers.') ? field.slice('headers.'.length) : 'list-id'
  let kind = field.startsWith('headers.') ? 'headers' : field
  let busy = false
  let error = ''
  let report: MatchReport | null = null

  $: folders = [
    ...roles,
    ...Object.values($sidebar.data?.foldersByAccount ?? {})
      .flat()
      .map((f) => f.imapPath || f.name),
  ].filter((f, i, all) => all.indexOf(f) === i)
  $: fieldItems = ['subject', 'from', 'to', 'body', 'headers'].map((f) => ({ value: f, label: $t(`scripts.match.field.${f}`) }))
  $: summaryText =
    report && !error
      ? `${$t('scripts.match.count').replace('{n}', String(report.matched)).replace('{m}', String(report.scanned))}${report.timedOut ? `. ${$t('scripts.match.timedOut')}` : ''}`
      : ''
  $: patternProblem =!when && pattern ? re2Problem(pattern) : ''

  async function run(): Promise<void> {
    busy = true
    error = ''
    try {
      report = await testScriptMatch({
        folder: folder.trim() || 'inbox',
        accountId: 0,
        limit,
        pattern: when ? '' : pattern,
        field: kind === 'headers' ? `headers.${header.trim().toLowerCase()}` : kind,
        when: when ?? undefined,
      })
    } catch (err) {
      report = null
      error = errorMessage(err)
    } finally {
      busy = false
    }
  }

  // splits a match's text into plain and marked parts by its spans.
  function parts(m: MatchHit): { text: string; mark: boolean }[] {
    const text = m.text ?? ''
    const out: { text: string; mark: boolean }[] = []
    let at = 0
    for (const [from, to] of m.spans) {
      if (from > at) {
        out.push({ text: text.slice(at, from), mark: false })
      }
      out.push({ text: text.slice(from, to), mark: true })
      at = to
    }
    if (at < text.length) {
      out.push({ text: text.slice(at), mark: false })
    }
    return out
  }
</script>

<form class="tester" on:submit|preventDefault={() => void run()}>
  <p class="hint">{$t(when ? 'scripts.match.hint.condition' : 'scripts.match.hint.pattern')}</p>
  {#if !when}
    <div class="row">
      <label class="field grow">
        <span class="label">{$t('scripts.match.pattern')}</span>
        <input class="cal-field mono" type="text" spellcheck="false" autocomplete="off" bind:value={pattern} aria-invalid={!!patternProblem} aria-describedby={patternProblem ? `${idBase}-problem` : undefined} />
      </label>
      <div class="field">
        <span class="label" id={`${idBase}-field`}>{$t('scripts.match.field')}</span>
        <Select ariaLabel={$t('scripts.match.field')} value={kind} items={fieldItems} on:change={(e) => (kind = e.detail)} />
      </div>
      {#if kind === 'headers'}
        <label class="field">
          <span class="label">{$t('scripts.match.header')}</span>
          <input class="cal-field mono" type="text" spellcheck="false" bind:value={header} />
        </label>
      {/if}
    </div>
    {#if patternProblem}<p class="problem" id={`${idBase}-problem`}>{$t(patternProblem)}</p>{/if}
  {/if}
  <div class="row">
    <label class="field grow">
      <span class="label">{$t('scripts.match.folder')}</span>
      <input class="cal-field" type="text" list={`${idBase}-folders`} spellcheck="false" bind:value={folder} />
      <datalist id={`${idBase}-folders`}>
        {#each folders as f (f)}<option value={f}></option>{/each}
      </datalist>
    </label>
    <div class="field">
      <span class="label">{$t('scripts.match.limit')}</span>
      <Select ariaLabel={$t('scripts.match.limit')} value={String(limit)} items={limits.map((n) => ({ value: String(n), label: String(n) }))} on:change={(e) => (limit = Number(e.detail))} />
    </div>
    <button type="submit" class="cal-btn primary" disabled={busy || (!when && (!pattern || !!patternProblem))}>{$t('scripts.match.run')}</button>
  </div>

  <Live text={error} assertive />
  <Live text={summaryText} />
  {#if error}
    <p class="problem">{error}</p>
  {:else if report}
    <p class="summary">
      {$t('scripts.match.count').replace('{n}', String(report.matched)).replace('{m}', String(report.scanned))}
      {#if report.timedOut}<span class="warn">{$t('scripts.match.timedOut')}</span>{/if}
    </p>
    {#if report.matches.length > 0}
      <ul class="hits">
        {#each report.matches as m (m.messageId)}
          <li>
            <div class="top">
              <span class="subject">{m.subject || $t('scripts.match.noSubject')}</span>
              <span class="date">{formatDateTimeMedium(new Date(m.date * 1000), true)}</span>
            </div>
            <span class="from">{m.from}</span>
            {#if m.text}
              <p class="excerpt">{#each parts(m) as p, i (i)}{#if p.mark}<mark>{p.text}</mark>{:else}{p.text}{/if}{/each}</p>
            {/if}
          </li>
        {/each}
      </ul>
      {#if report.matched > report.matches.length}
        <p class="hint">{$t('scripts.match.more').replace('{n}', String(report.matched - report.matches.length))}</p>
      {/if}
    {/if}
  {/if}
</form>

<style>
  .tester {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    min-width: 0;
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: var(--space-2);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .grow {
    flex: 1;
    min-width: calc(var(--control-height) * 5);
  }

  .label {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .mono {
    font-family: var(--font-mono);
  }

  .hint {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .problem {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--danger);
  }

  .summary {
    margin: 0;
    display: flex;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .warn {
    color: var(--warning);
  }

  .hits {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    max-height: var(--automation-steps-max);
    overflow-y: auto;
  }

  .hits li {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--space-2);
    border-inline-start: 2px solid var(--border-default);
    background: var(--surface-sunken);
    border-radius: var(--radius-control);
  }

  .top {
    display: flex;
    justify-content: space-between;
    gap: var(--space-2);
    font-size: var(--fz-label);
  }

  .subject {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-primary);
  }

  .date,
  .from {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .excerpt {
    margin: 0;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    color: var(--text-secondary);
  }

  mark {
    background: var(--highlight-bg);
    color: var(--text-primary);
    border-radius: var(--radius-control);
  }
</style>
