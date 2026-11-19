<script lang="ts">
  // one feed's settings: its name, folder and refresh interval, whether its
  // images load without asking and whether new entries notify, with the
  // feed's address, when it was last fetched and why that failed if it did.
  // unsubscribing asks first, since the entries go with the feed.
  import { createEventDispatcher } from 'svelte'
  import '../calendar/controls.css'
  import Modal from '../common/Modal.svelte'
  import Select from '../common/Select.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import { deleteFeed, refreshFeed, updateFeed } from '../../lib/api'
  import { formatFullDate, type TimeFormat } from '../../lib/format'
  import { askConfirm } from '../../stores/confirm'
  import { prefs } from '../../stores/prefs'
  import { errorMessage, toastError } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { Feed, FeedFolder } from '../../lib/types'

  export let feed: Feed
  export let folders: FeedFolder[]

  const dispatch = createEventDispatcher<{ close: void; saved: void; removed: number }>()

  let name = feed.customTitle
  let folder = String(feed.folderId)
  let interval = String(feed.intervalMinutes)
  let loadImages = feed.loadImages
  let notify = feed.notify
  let saving = false

  $: dirty =
    name !== feed.customTitle ||
    folder !== String(feed.folderId) ||
    interval !== String(feed.intervalMinutes) ||
    loadImages !== feed.loadImages ||
    notify !== feed.notify
  $: timeFormat = $prefs.timeFormat as TimeFormat
  $: folderItems = [{ value: '0', label: $t('feeds.noFolder') }, ...folders.map((f) => ({ value: String(f.id), label: f.name }))]
  $: intervalItems = [
    { value: '0', label: $t('feeds.settings.intervalDefault').replace('{n}', String($prefs.feedsRefreshMinutes)) },
    ...[15, 30, 60, 120, 360, 720, 1440].map((m) => ({ value: String(m), label: intervalLabel(m) })),
  ]

  function intervalLabel(minutes: number): string {
    return minutes < 60 ? $t('feeds.minutes').replace('{n}', String(minutes)) : $t('feeds.hours').replace('{n}', String(minutes / 60))
  }

  async function save(): Promise<void> {
    saving = true
    try {
      await updateFeed({ id: feed.id, customTitle: name.trim(), folderId: Number(folder), intervalMinutes: Number(interval), loadImages, notify })
      dispatch('saved')
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      saving = false
    }
  }

  async function unsubscribe(): Promise<void> {
    const ok = await askConfirm({
      title: $t('feeds.unsubscribe.title').replace('{name}', feed.title),
      body: $t('feeds.unsubscribe.body'),
      confirmLabel: $t('feeds.unsubscribe'),
      danger: true,
    })
    if (!ok) {
      return
    }
    try {
      await deleteFeed(feed.id)
      dispatch('removed', feed.id)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function refreshNow(): Promise<void> {
    try {
      await refreshFeed(feed.id)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<Modal title={$t('feeds.settings.title').replace('{name}', feed.title)} {dirty} on:close={() => dispatch('close')}>
  <div class="form">
    <label class="field">
      <span>{$t('feeds.add.name')}</span>
      <input class="cal-field" bind:value={name} placeholder={feed.feedTitle || feed.url} />
    </label>
    <div class="pair">
      <div class="field">
        <span>{$t('feeds.settings.folder')}</span>
        <Select value={folder} items={folderItems} ariaLabel={$t('feeds.settings.folder')} on:change={(e) => (folder = e.detail)} />
      </div>
      <div class="field">
        <span>{$t('feeds.settings.interval')}</span>
        <Select value={interval} items={intervalItems} ariaLabel={$t('feeds.settings.interval')} on:change={(e) => (interval = e.detail)} />
      </div>
    </div>
    <div class="switch-row">
      <div class="switch-text">
        <span class="label">{$t('feeds.settings.images')}</span>
        <span class="hint">{$t('feeds.settings.imagesHint')}</span>
      </div>
      <ToggleSwitch checked={loadImages} label={$t('feeds.settings.images')} on:change={(e) => (loadImages = e.detail)} />
    </div>
    <div class="switch-row">
      <div class="switch-text">
        <span class="label">{$t('feeds.settings.notify')}</span>
        <span class="hint">{$t('feeds.settings.notifyHint')}</span>
      </div>
      <ToggleSwitch checked={notify} label={$t('feeds.settings.notify')} on:change={(e) => (notify = e.detail)} />
    </div>

    <dl class="facts">
      <dt>{$t('feeds.settings.address')}</dt>
      <dd class="mono">{feed.url}</dd>
      {#if feed.siteUrl}
        <dt>{$t('feeds.settings.site')}</dt>
        <dd class="mono">{feed.siteUrl}</dd>
      {/if}
      <dt>{$t('feeds.settings.lastFetched')}</dt>
      <dd>{feed.lastFetched ? formatFullDate(feed.lastFetched, timeFormat) : $t('feeds.settings.never')}</dd>
      {#if feed.error}
        <dt>{$t('feeds.settings.lastError')}</dt>
        <dd class="error">{feed.error}</dd>
      {/if}
    </dl>
  </div>

  <svelte:fragment slot="footer">
    <div class="buttons">
      <button type="button" class="cal-btn danger" on:click={() => void unsubscribe()}>{$t('feeds.unsubscribe')}</button>
      <button type="button" class="cal-btn" on:click={() => void refreshNow()}>{$t('feeds.menu.refresh')}</button>
      <span class="spacer"></span>
      <button type="button" class="cal-btn" on:click={() => dispatch('close')}>{$t('feeds.cancel')}</button>
      <button type="button" class="cal-btn primary" disabled={!dirty || saving} on:click={() => void save()}>{$t('feeds.save')}</button>
    </div>
  </svelte:fragment>
</Modal>

<style>
  .form {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .pair {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(calc(var(--feed-dialog-width) / 3), 1fr));
    gap: var(--space-3);
  }

  .switch-row {
    display: flex;
    align-items: center;
    gap: var(--space-4);
  }

  .switch-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1;
    min-width: 0;
  }

  .label {
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .hint {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .facts {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--space-1) var(--space-4);
    margin: 0;
    padding-top: var(--space-3);
    border-top: var(--hairline) solid var(--border-subtle);
    font-size: var(--fz-label);
  }

  dt {
    color: var(--text-tertiary);
  }

  dd {
    margin: 0;
    min-width: 0;
    color: var(--text-secondary);
    overflow-wrap: anywhere;
  }

  .mono {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
  }

  dd.error {
    color: var(--danger);
  }

  .buttons {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
  }

  .spacer {
    flex: 1;
  }
</style>
