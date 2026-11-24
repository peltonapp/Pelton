<script lang="ts">
  // the tracking report: every sender by how much their mail tried to track
  // the reader, over a period, with the mailbox totals on top and the state of
  // each unsubscribe. It takes the place of the message list and the reading
  // pane like the newsletter manager; Escape or the close button brings those
  // back. Everything comes from counters kept on this device.
  import { createEventDispatcher, onDestroy, onMount } from 'svelte'
  import { get } from 'svelte/store'
  import shieldIcon from '@phosphor-icons/core/assets/duotone/shield-check-duotone.svg?raw'
  import funnelIcon from '@phosphor-icons/core/assets/duotone/funnel-simple-duotone.svg?raw'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import Ph from '../common/Ph.svelte'
  import Segmented from '../settings2/ui/Segmented.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import { t } from '../../lib/i18n'
  import { fill } from '../../lib/passport'
  import { filterSenders, reportSorts, sortSenders, type ReportSort } from '../../lib/privacyreport'
  import { blockUnsubscribedSender, clearPrivacyHistory, dismissStillSending, privacyReport } from '../../lib/api'
  import { onPrivacyChanged } from '../../lib/events'
  import { popupIsOpen } from '../../lib/popups'
  import { askConfirm, confirmRequest } from '../../stores/confirm'
  import { prefs, setPrivacyCounters } from '../../stores/prefs'
  import { reportFilter } from '../../stores/trust'
  import { errorMessage, toastError } from '../../stores/toast'
  import type { PrivacyReport, PrivacySender } from '../../lib/types'

  const dispatch = createEventDispatcher<{ close: void }>()

  const periods = [7, 30, 90, 365, 0]
  let days = 30
  let sort: ReportSort = 'tracking'
  let filter = get(reportFilter)
  reportFilter.set('')

  let report: PrivacyReport | null = null
  let error = ''
  let busy = false
  let announce = ''
  let request = 0

  async function load(): Promise<void> {
    const mine = ++request
    try {
      const r = await privacyReport(days)
      if (mine === request) {
        report = r
        error = ''
      }
    } catch (err) {
      if (mine === request) {
        error = errorMessage(err)
      }
    }
  }

  $: days, void load()

  let stop: (() => void) | undefined
  onMount(() => {
    stop = onPrivacyChanged(() => void load())
  })
  onDestroy(() => stop?.())

  $: periodOptions = periods.map((d) => ({ key: String(d), label: d === 0 ? $t('privacy.report.all') : fill($t('privacy.report.days'), { n: d }) }))
  $: sortOptions = reportSorts.map((s) => ({ key: s, label: $t(`privacy.report.sort.${s}`) }))
  $: rows = report ? sortSenders(filterSenders(report.rows, filter), sort) : []
  $: summary = report
    ? fill($t('privacy.report.summary'), {
        trackers: report.trackers,
        senders: report.senders,
        period: days === 0 ? $t('privacy.report.allTime') : fill($t('privacy.report.inDays'), { n: days }),
      })
    : ''

  function since(s: PrivacySender): string {
    const d = new Date(s.first || s.unsub?.at || '')
    return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString(undefined, { year: 'numeric', month: 'short' })
  }

  async function act(run: () => Promise<void>, done: string): Promise<void> {
    busy = true
    try {
      await run()
      announce = done
      await load()
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  async function clearHistory(): Promise<void> {
    const ok = await askConfirm({ title: $t('privacy.report.clear'), body: $t('privacy.report.clearConfirm'), confirmLabel: $t('privacy.report.clear'), danger: true })
    if (ok) {
      await act(clearPrivacyHistory, $t('privacy.report.clear'))
    }
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key !== 'Escape' || event.defaultPrevented || popupIsOpen() || get(confirmRequest) !== null) {
      return
    }
    const target = event.target as HTMLElement | null
    if (target?.closest?.('input') && filter !== '') {
      filter = ''
      return
    }
    dispatch('close')
  }
</script>

<svelte:window on:keydown={onKeydown} />

<section class="report" aria-labelledby="privacy-report-title">
  <header class="head">
    <div class="title">
      <Ph svg={shieldIcon} size={18} />
      <h2 id="privacy-report-title">{$t('privacy.report.title')}</h2>
    </div>
    <button type="button" class="cal-icon" aria-label={$t('privacy.report.close')} title={$t('privacy.report.close')} on:click={() => dispatch('close')}>
      <Ph svg={xIcon} size={15} />
    </button>
  </header>

  {#if report}
    <div class="totals">
      <p class="summary">{summary}</p>
      <p class="sub">{fill($t('privacy.report.links'), { n: report.links, mails: report.mails })}</p>
      {#if !report.counting}
        <p class="sub off">{$t('privacy.report.off')}</p>
      {/if}
    </div>
  {/if}

  <div class="bar">
    <label class="filter">
      <Ph svg={funnelIcon} size={13} />
      <input type="text" autocomplete="off" spellcheck="false" placeholder={$t('privacy.report.filter')} aria-label={$t('privacy.report.filter')} bind:value={filter} />
    </label>
    <Segmented value={String(days)} options={periodOptions} label={$t('privacy.report.period')} on:change={(e) => (days = Number(e.detail))} />
    <Segmented value={sort} options={sortOptions} label={$t('privacy.report.sort')} on:change={(e) => (sort = e.detail as ReportSort)} />
    <span class="grow"></span>
    <ToggleSwitch checked={$prefs.privacyCounters} label={$t('privacy.counters.label')} on:change={(e) => setPrivacyCounters(e.detail)} />
    <span class="toggle-label" aria-hidden="true">{$t('privacy.counters.label')}</span>
    <button type="button" class="cal-btn danger" disabled={busy} on:click={clearHistory}>{$t('privacy.report.clear')}</button>
  </div>

  <div class="scroll">
    {#if error}
      <p class="state" role="alert">{error}</p>
    {:else if report && rows.length === 0}
      <p class="state">{$t('privacy.report.empty')}</p>
    {:else if report}
      <table>
        <thead>
          <tr>
            <th scope="col">{$t('privacy.report.col.sender')}</th>
            <th scope="col" class="num">{$t('privacy.report.col.mails')}</th>
            <th scope="col" class="num">{$t('privacy.report.col.pixels')}</th>
            <th scope="col" class="num">{$t('privacy.report.col.links')}</th>
            <th scope="col" class="num">{$t('privacy.report.col.receipts')}</th>
            <th scope="col">{$t('privacy.report.col.since')}</th>
            <th scope="col">{$t('privacy.report.col.status')}</th>
          </tr>
        </thead>
        <tbody>
          {#each rows as s (s.address)}
            <tr>
              <th scope="row" class="who">
                <span class="name" dir="auto">{s.name || s.address}</span>
                {#if s.name}<span class="addr" dir="ltr">{s.address}</span>{/if}
                {#if s.companies.length > 0}<span class="companies">{s.companies.join(', ')}</span>{/if}
              </th>
              <td class="num">{s.mails}</td>
              <td class="num">{s.pixels}</td>
              <td class="num">{s.links}</td>
              <td class="num">{s.receipts}</td>
              <td>{since(s)}</td>
              <td class="status">
                {#if s.unsub}
                  {@const u = s.unsub}
                  <span>{fill($t('shield.unsub.done'), { date: new Date(u.at).toLocaleDateString() })}</span>
                  {#if u.stillSending}
                    <span class="still">{fill($t('shield.unsub.still'), { name: s.name || s.address, date: new Date(u.at).toLocaleDateString(undefined, { day: 'numeric', month: 'short' }) })}</span>
                    <span class="acts">
                      {#if u.blocked === ''}
                        <button type="button" class="cal-btn" disabled={busy} on:click={() => act(() => blockUnsubscribedSender(u.sender, 'junk'), $t('shield.unsub.blocked.junk'))}>{$t('shield.unsub.junk')}</button>
                      {/if}
                      <button type="button" class="cal-btn" disabled={busy} on:click={() => act(() => dismissStillSending(u.sender), $t('shield.unsub.dismiss'))}>{$t('shield.unsub.dismiss')}</button>
                    </span>
                  {/if}
                  {#if u.blocked !== ''}
                    <span>{$t(`shield.unsub.blocked.${u.blocked}`)}</span>
                  {/if}
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
  <span class="sr-only" role="status" aria-live="polite">{announce}</span>
</section>

<style>
  .report {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: var(--surface-raised);
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .title {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--text-secondary);
  }

  h2 {
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .totals {
    padding: var(--space-3) var(--space-4) 0;
  }

  .summary {
    margin: 0;
    font-size: var(--fz-body);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
  }

  .sub {
    margin: var(--space-1) 0 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .sub.off {
    color: var(--warning);
  }

  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .grow {
    flex: 1;
  }

  .toggle-label {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .filter {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    color: var(--text-tertiary);
  }

  .filter input {
    min-width: 0;
    width: 14em;
    height: var(--control-height);
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    outline: none;
  }

  .filter:focus-within {
    border-color: var(--accent);
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow: auto;
  }

  .state {
    margin: 0;
    padding: var(--space-6) var(--space-4);
    text-align: center;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--fz-label);
  }

  th,
  td {
    padding: var(--space-2) var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
    text-align: start;
    vertical-align: top;
  }

  thead th {
    position: sticky;
    top: 0;
    background: var(--surface-raised);
    font-weight: var(--fw-medium);
    color: var(--text-tertiary);
  }

  .num {
    text-align: end;
    font-variant-numeric: tabular-nums;
  }

  .who {
    display: flex;
    flex-direction: column;
    font-weight: var(--fw-regular);
  }

  .name {
    color: var(--text-primary);
  }

  .addr,
  .companies {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .status {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    color: var(--text-secondary);
  }

  .still {
    color: var(--warning);
  }

  .acts {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }
</style>
