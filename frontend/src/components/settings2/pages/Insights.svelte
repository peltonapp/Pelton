<script lang="ts">
  // the Insights page: statistics about the mailbox, computed by the backend
  // from the local store. Each section loads on its own behind a pie loader and
  // is kept for the session, so switching ranges back is instant.
  import refreshIcon from '@phosphor-icons/core/assets/duotone/arrows-clockwise-duotone.svg?raw'
  import Card from '../ui/Card.svelte'
  import Segmented from '../ui/Segmented.svelte'
  import Ph from '../../common/Ph.svelte'
  import PieProgress from '../../common/PieProgress.svelte'
  import BarChart from '../../insights/BarChart.svelte'
  import HeatGrid from '../../insights/HeatGrid.svelte'
  import RankList from '../../insights/RankList.svelte'
  import CountUp from '../../insights/CountUp.svelte'
  import { t, locale } from '../../../lib/i18n'
  import { tip } from '../../../lib/tip'
  import { formatBytes, formatListDate, type TimeFormat } from '../../../lib/format'
  import { openMessageWindow } from '../../../lib/api'
  import {
    busiest,
    formatDuration,
    gridWeekdays,
    hourGrid,
    insightsRanges,
    localDay,
    senderLabel,
    volumeBuckets,
    type InsightsPeople,
    type InsightsRange,
    type InsightsReply,
    type InsightsStorage,
    type InsightsVolume,
    type VolumeBucket,
  } from '../../../lib/mailstats'
  import { clearInsights, peopleInsights, storageInsights, volumeInsights } from '../../../stores/insights'
  import { prefs } from '../../../stores/prefs'
  import { errorMessage } from '../../../stores/toast'

  let range: InsightsRange = '30'
  let reload = 0

  // naming reload makes the refresh button rerun these.
  function track<T>(_reload: number, work: Promise<T>): Promise<T> {
    return work
  }

  $: storage = track(reload, storageInsights())
  $: volume = track(reload, volumeInsights(range))
  $: people = track(reload, peopleInsights(range))
  $: rangeOptions = insightsRanges.map((key) => ({ key, label: $t(`insights.range.${key}`) }))

  // 2026-01-04 was a Sunday, so adding a getDay number lands on that weekday.
  $: weekdayNames = gridWeekdays.map((d) =>
    new Date(2026, 0, 4 + d).toLocaleDateString($locale, { weekday: 'short' }),
  )
  $: longWeekday = (d: number): string => new Date(2026, 0, 4 + d).toLocaleDateString($locale, { weekday: 'long' })

  function refresh(): void {
    clearInsights()
    reload += 1
  }

  function totalBytes(s: InsightsStorage): number {
    return s.accounts.reduce((sum, a) => sum + a.bytes, 0)
  }

  function dayText(day: string, opts: Intl.DateTimeFormatOptions): string {
    const [y, m, d] = day.split('-').map(Number)
    return new Date(y, m - 1, d).toLocaleDateString($locale, opts)
  }

  function barLabel(b: VolumeBucket, r: InsightsRange): string {
    if (r === '365') {
      return $t('insights.volume.weekOf').replace('{date}', dayText(b.start, { day: 'numeric', month: 'short', year: 'numeric' }))
    }
    if (r === 'all') {
      return dayText(b.start, { month: 'long', year: 'numeric' })
    }
    return dayText(b.start, { weekday: 'short', day: 'numeric', month: 'short' })
  }

  function bars(v: InsightsVolume, r: InsightsRange): { label: string; received: number; sent: number }[] {
    return volumeBuckets(v.days, r, localDay(new Date())).map((b) => ({ label: barLabel(b, r), received: b.received, sent: b.sent }))
  }

  function sum(v: InsightsVolume, key: 'received' | 'sent'): number {
    return v.days.reduce((n, d) => n + d[key], 0)
  }

  function hourText(hour: number): string {
    return new Date(2026, 0, 5, hour).toLocaleTimeString($locale, {
      hour: 'numeric',
      ...($prefs.timeFormat === '12' ? { hour12: true } : $prefs.timeFormat === '24' ? { hourCycle: 'h23' as const } : {}),
    })
  }

  function cellText(day: string, hour: number, count: number): string {
    return $t('insights.volume.cell').replace('{day}', day).replace('{hour}', hourText(hour)).replace('{n}', count.toLocaleString())
  }

  function busiestText(v: InsightsVolume): string {
    const top = busiest(v.hours)
    if (!top) {
      return ''
    }
    return $t('insights.volume.busiest').replace('{day}', longWeekday(top.weekday)).replace('{hour}', hourText(top.hour))
  }

  function replyText(r: InsightsReply): string {
    return formatDuration(r.medianSeconds, $t)
  }

  function personItems(list: InsightsPeople['writesTo']): { key: string; label: string; sub: string; value: number }[] {
    return list.map((p) => ({ key: p.email, label: senderLabel(p), sub: p.name ? p.email : '', value: p.count }))
  }
</script>

<p class="local">{$t('insights.local')}</p>

<div class="toolbar">
  <Segmented value={range} options={rangeOptions} label={$t('insights.range.label')} on:change={(e) => (range = e.detail as InsightsRange)} />
  <button type="button" class="icon" aria-label={$t('insights.refresh')} use:tip={{ text: $t('insights.refresh') }} on:click={refresh}>
    <Ph svg={refreshIcon} size={15} />
  </button>
</div>

<Card title={$t('insights.storage.title')} id="insights-storage" bare>
  {#await storage}
    <div class="loading"><PieProgress size={18} label={$t('insights.loading')} /></div>
  {:then s}
    <div class="figures">
      <div class="figure">
        <span class="big"><CountUp value={totalBytes(s)} format={formatBytes} /></span>
        <span class="caption">{$t('insights.storage.total')}</span>
      </div>
      <div class="figure">
        <span class="big"><CountUp value={s.accounts.reduce((n, a) => n + a.count, 0)} /></span>
        <span class="caption">{$t('insights.storage.messages')}</span>
      </div>
    </div>
    <div class="grid">
      {#each s.accounts as account (account.id)}
        <section class="panel">
          <h4>
            <span>{account.local ? $t('insights.storage.local') : account.label || account.email}</span>
            <span class="meta">{formatBytes(account.bytes)}</span>
          </h4>
          <RankList
            items={account.folders.slice(0, 8).map((f) => ({ key: String(f.id), label: f.name, sub: $t('insights.messages').replace('{n}', f.count.toLocaleString()), value: f.bytes }))}
            format={formatBytes}
            empty={$t('insights.storage.noMail')}
          />
        </section>
      {/each}
      <section class="panel">
        <h4>{$t('insights.storage.senders')}</h4>
        <RankList
          items={s.senders.map((p) => ({ key: p.email, label: senderLabel(p), sub: $t('insights.messages').replace('{n}', p.count.toLocaleString()), value: p.bytes }))}
          format={formatBytes}
          empty={$t('insights.storage.noMail')}
        />
      </section>
      <section class="panel">
        <h4>{$t('insights.storage.attachments')}</h4>
        <RankList
          items={s.attachments.map((a) => ({
            key: String(a.id),
            label: a.filename,
            sub: `${a.from} · ${formatListDate(a.date, $prefs.timeFormat as TimeFormat)}`,
            value: a.bytes,
            action: () => void openMessageWindow(a.messageId, a.subject),
          }))}
          format={formatBytes}
          empty={$t('insights.storage.noAttachments')}
        />
      </section>
    </div>
  {:catch err}
    <p class="error" role="alert">{errorMessage(err)}</p>
  {/await}
</Card>

<Card title={$t('insights.volume.title')} id="insights-volume" bare>
  {#await volume}
    <div class="loading"><PieProgress size={18} label={$t('insights.loading')} /></div>
  {:then v}
    <div class="figures">
      <div class="figure">
        <span class="big"><CountUp value={sum(v, 'received')} /></span>
        <span class="caption">{$t('insights.volume.received')}</span>
      </div>
      {#if v.hasSent}
        <div class="figure">
          <span class="big"><CountUp value={sum(v, 'sent')} /></span>
          <span class="caption">{$t('insights.volume.sent')}</span>
        </div>
      {/if}
    </div>
    {#if v.days.length === 0}
      <p class="empty">{$t('insights.volume.none')}</p>
    {:else}
      <div class="panel pad">
        {#key range}
          <BarChart
            bars={bars(v, range)}
            label={$t('insights.volume.chart')}
            receivedLabel={$t('insights.volume.receivedShort')}
            sentLabel={$t('insights.volume.sentShort')}
          />
        {/key}
      </div>
      <div class="panel pad">
        <h4 class="plain">{busiestText(v)}</h4>
        <HeatGrid grid={hourGrid(v.hours)} days={weekdayNames} label={`${$t('insights.volume.grid')}. ${busiestText(v)}`} {cellText} />
      </div>
    {/if}
  {:catch err}
    <p class="error" role="alert">{errorMessage(err)}</p>
  {/await}
</Card>

<Card title={$t('insights.people.title')} id="insights-people" bare>
  {#await people}
    <div class="loading"><PieProgress size={18} label={$t('insights.loading')} /></div>
  {:then p}
    <div class="grid">
      <section class="panel">
        <h4>{$t('insights.people.writesTo')}</h4>
        <RankList items={personItems(p.writesTo)} empty={$t('insights.people.none')} />
      </section>
      <section class="panel">
        <h4>{$t('insights.people.writesYou')}</h4>
        <RankList items={personItems(p.writesYou)} empty={$t('insights.people.none')} />
      </section>
    </div>
    <div class="figures replies">
      {#each [{ r: p.myReplies, key: 'mine' }, { r: p.theirReplies, key: 'theirs' }] as side (side.key)}
        <div class="figure">
          {#if side.r.available}
            <span class="big">{replyText(side.r)}</span>
            <span class="caption">
              {$t(`insights.replies.${side.key}`)}
              <span class="meta">{$t('insights.replies.over').replace('{n}', side.r.count.toLocaleString())}</span>
            </span>
          {:else}
            <span class="caption">{$t(`insights.replies.${side.key}`)}</span>
            <span class="meta">{$t('insights.replies.notEnough')}</span>
          {/if}
        </div>
      {/each}
    </div>
  {:catch err}
    <p class="error" role="alert">{errorMessage(err)}</p>
  {/await}
</Card>

<style>
  .local {
    margin: 0 0 var(--space-4);
    color: var(--text-tertiary);
    font-size: var(--fz-label);
  }

  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    margin-bottom: var(--space-5);
  }

  .icon {
    display: inline-flex;
    padding: var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }

  .icon:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .loading {
    display: flex;
    justify-content: center;
    padding: var(--space-6);
    color: var(--text-secondary);
  }

  .figures {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-6);
    margin-bottom: var(--space-4);
  }

  .figure {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .big {
    font-size: var(--fz-title);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
    font-variant-numeric: tabular-nums;
  }

  .caption {
    color: var(--text-secondary);
    font-size: var(--fz-label);
  }

  .meta {
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
    font-weight: var(--fw-regular);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(calc(var(--space-6) * 11), 1fr));
    gap: var(--space-4);
  }

  .panel {
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    overflow: hidden;
  }

  .panel.pad {
    padding: var(--space-4);
    margin-bottom: var(--space-4);
  }

  h4 {
    display: flex;
    justify-content: space-between;
    gap: var(--space-3);
    margin: 0;
    padding: var(--space-3) var(--space-4) var(--space-2);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  h4.plain {
    padding: 0 0 var(--space-3);
    font-weight: var(--fw-regular);
    color: var(--text-secondary);
  }

  .replies {
    margin-top: var(--space-5);
  }

  .empty,
  .error {
    margin: 0;
    color: var(--text-tertiary);
    font-size: var(--fz-label);
  }

  .error {
    color: var(--danger);
  }
</style>
