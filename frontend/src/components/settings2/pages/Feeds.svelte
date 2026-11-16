<script lang="ts">
  // the Feeds page: how often feeds are fetched, how long entries are kept,
  // whether feed images load without asking, whether scrolling past an entry
  // marks it read, and the OPML import and export of the subscriptions.
  // notifications are opted into per feed, in that feed's settings.
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import Select from '../../common/Select.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import { exportOpml, importOpml } from '../../../lib/api'
  import { loadFeedSidebar, refreshAllFeeds } from '../../../stores/feeds'
  import { prefs, setFeedPref } from '../../../stores/prefs'
  import { errorMessage, toastError, toastSuccess } from '../../../stores/toast'
  import { t } from '../../../lib/i18n'

  let busy = false

  function every(minutes: number): string {
    return minutes < 60 ? $t('feeds.minutes').replace('{n}', String(minutes)) : $t('feeds.hours').replace('{n}', String(minutes / 60))
  }

  $: intervalItems = [
    { value: '0', label: $t('feedSettings.interval.manual') },
    ...[15, 30, 60, 120, 360, 720, 1440].map((m) => ({ value: String(m), label: every(m) })),
  ]
  $: dayItems = [
    ...[7, 14, 30, 90, 180, 365].map((d) => ({ value: String(d), label: $t('feedSettings.days').replace('{n}', String(d)) })),
    { value: '0', label: $t('feedSettings.forever') },
  ]
  $: countItems = [
    ...[50, 100, 200, 500, 1000, 5000].map((n) => ({ value: String(n), label: $t('feedSettings.entries').replace('{n}', String(n)) })),
    { value: '0', label: $t('feedSettings.noLimit') },
  ]

  function withCurrent(items: { value: string; label: string }[], value: number, label: (n: number) => string): { value: string; label: string }[] {
    return items.some((i) => i.value === String(value)) ? items : [...items, { value: String(value), label: label(value) }]
  }

  async function runImport(): Promise<void> {
    busy = true
    try {
      const res = await importOpml()
      if (res.added > 0 || res.skipped > 0) {
        toastSuccess($t('feeds.imported').replace('{added}', String(res.added)).replace('{skipped}', String(res.skipped)))
        await loadFeedSidebar()
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  async function runExport(): Promise<void> {
    busy = true
    try {
      if (await exportOpml()) {
        toastSuccess($t('feeds.exported'))
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }
</script>

<Card title={$t('feedSettings.refreshing')}>
  <Row id="feeds-interval" label={$t('feedSettings.interval')} hint={$t('feedSettings.interval.hint')}>
    <Select
      value={String($prefs.feedsRefreshMinutes)}
      items={withCurrent(intervalItems, $prefs.feedsRefreshMinutes, every)}
      ariaLabel={$t('feedSettings.interval')}
      on:change={(e) => setFeedPref('feedsRefreshMinutes', Number(e.detail))}
    />
  </Row>
  <Row id="feeds-refresh-now" label={$t('feeds.refreshAll')} hint={$t('feedSettings.refreshNow.hint')}>
    <button type="button" class="s2-btn" on:click={() => void refreshAllFeeds()}>{$t('feeds.refresh')}</button>
  </Row>
</Card>

<Card title={$t('feedSettings.keeping')} description={$t('feedSettings.keeping.description')}>
  <Row id="feeds-keep-days" label={$t('feedSettings.keepDays')} hint={$t('feedSettings.keepDays.hint')}>
    <Select
      value={String($prefs.feedsKeepDays)}
      items={withCurrent(dayItems, $prefs.feedsKeepDays, (n) => $t('feedSettings.days').replace('{n}', String(n)))}
      ariaLabel={$t('feedSettings.keepDays')}
      on:change={(e) => setFeedPref('feedsKeepDays', Number(e.detail))}
    />
  </Row>
  <Row id="feeds-keep-count" label={$t('feedSettings.keepCount')} hint={$t('feedSettings.keepCount.hint')}>
    <Select
      value={String($prefs.feedsKeepCount)}
      items={withCurrent(countItems, $prefs.feedsKeepCount, (n) => $t('feedSettings.entries').replace('{n}', String(n)))}
      ariaLabel={$t('feedSettings.keepCount')}
      on:change={(e) => setFeedPref('feedsKeepCount', Number(e.detail))}
    />
  </Row>
</Card>

<Card title={$t('feedSettings.reading')}>
  <Row id="feeds-images" label={$t('feedSettings.images')} hint={$t('feedSettings.images.hint')}>
    <ToggleSwitch checked={$prefs.feedsLoadImages} label={$t('feedSettings.images')} on:change={(e) => setFeedPref('feedsLoadImages', e.detail)} />
  </Row>
  <Row id="feeds-mark-scroll" label={$t('feedSettings.markOnScroll')} hint={$t('feedSettings.markOnScroll.hint')}>
    <ToggleSwitch checked={$prefs.feedsMarkReadOnScroll} label={$t('feedSettings.markOnScroll')} on:change={(e) => setFeedPref('feedsMarkReadOnScroll', e.detail)} />
  </Row>
  <Row id="feeds-notify" label={$t('feedSettings.notify')} hint={$t('feedSettings.notify.hint')}>
    <span class="note">{$t('feedSettings.notify.perFeed')}</span>
  </Row>
</Card>

<Card title={$t('feedSettings.subscriptions')} description={$t('feedSettings.subscriptions.description')}>
  <Row id="feeds-opml" label={$t('feedSettings.opml')} hint={$t('feedSettings.opml.hint')}>
    <div class="buttons">
      <button type="button" class="s2-btn" disabled={busy} on:click={() => void runImport()}>{$t('feeds.importOpml')}</button>
      <button type="button" class="s2-btn" disabled={busy} on:click={() => void runExport()}>{$t('feeds.exportOpml')}</button>
    </div>
  </Row>
</Card>

<style>
  .buttons {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .note {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }
</style>
