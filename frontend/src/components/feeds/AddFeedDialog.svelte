<script lang="ts">
  // adding a feed: the user types a site or a feed address, Find fetches it
  // and lists the feeds found there with a preview of each (how many entries,
  // the latest titles, when it last posted), and Subscribe adds the chosen one
  // to a folder, under its own name or one the user gives it.
  import { createEventDispatcher, onMount, tick } from 'svelte'
  import rssIcon from '@phosphor-icons/core/assets/duotone/rss-duotone.svg?raw'
  import '../calendar/controls.css'
  import Modal from '../common/Modal.svelte'
  import Select from '../common/Select.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import Ph from '../common/Ph.svelte'
  import { addFeed, discoverFeeds } from '../../lib/api'
  import { formatListDate, type TimeFormat } from '../../lib/format'
  import { hostOf } from '../../lib/feedlogic'
  import { errorMessage } from '../../stores/toast'
  import { prefs } from '../../stores/prefs'
  import { t } from '../../lib/i18n'
  import type { Feed, FeedCandidate, FeedFolder } from '../../lib/types'

  /** The folders a feed can go into. */
  export let folders: FeedFolder[]
  /** The folder picked to start with, 0 for none. */
  export let folderId: number = 0
  /** An address to start with, such as one dropped or pasted in. */
  export let address: string = ''

  const dispatch = createEventDispatcher<{ close: void; added: Feed }>()

  let input: HTMLInputElement
  let finding = false
  let adding = false
  let error = ''
  let found: FeedCandidate[] = []
  let chosen = ''
  let title = ''
  let folder = String(folderId)

  onMount(async () => {
    await tick()
    input?.focus()
    if (address.trim()) {
      void find()
    }
  })

  $: folderItems = [{ value: '0', label: $t('feeds.noFolder') }, ...folders.map((f) => ({ value: String(f.id), label: f.name }))]
  $: pick = found.find((c) => c.url === chosen) ?? null
  $: timeFormat = $prefs.timeFormat as TimeFormat

  async function find(): Promise<void> {
    if (!address.trim() || finding) {
      return
    }
    finding = true
    error = ''
    found = []
    chosen = ''
    try {
      found = await discoverFeeds(address)
      chosen = found.find((c) => !c.subscribed)?.url ?? found[0]?.url ?? ''
    } catch (err) {
      error = errorMessage(err)
    } finally {
      finding = false
    }
  }

  async function subscribe(): Promise<void> {
    if (!pick || pick.subscribed || adding) {
      return
    }
    adding = true
    error = ''
    try {
      const feed = await addFeed(pick.url, Number(folder), title.trim())
      dispatch('added', feed)
    } catch (err) {
      error = errorMessage(err)
    } finally {
      adding = false
    }
  }

  function onKey(event: KeyboardEvent): void {
    if (event.key === 'Enter') {
      event.preventDefault()
      void find()
    }
  }
</script>

<Modal title={$t('feeds.add.title')} hint={$t('feeds.add.hint')} dirty={address.trim() !== '' && !adding} on:close={() => dispatch('close')}>
  <span slot="icon"><Ph svg={rssIcon} size={16} /></span>
  <div class="form">
    <div class="find">
      <input
        class="cal-field address"
        type="url"
        bind:this={input}
        bind:value={address}
        placeholder={$t('feeds.add.placeholder')}
        aria-label={$t('feeds.add.address')}
        spellcheck="false"
        autocapitalize="off"
        on:keydown={onKey}
      />
      <button type="button" class="cal-btn" disabled={!address.trim() || finding} on:click={() => void find()}>
        {#if finding}
          <PieProgress label={$t('feeds.add.finding')} />
        {/if}
        {$t('feeds.add.find')}
      </button>
    </div>

    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}

    {#if found.length > 0}
      <fieldset class="found">
        <legend>{found.length === 1 ? $t('feeds.add.foundOne') : $t('feeds.add.foundMany').replace('{n}', String(found.length))}</legend>
        {#each found as c (c.url)}
          <label class="candidate" class:on={chosen === c.url} class:taken={c.subscribed}>
            <input type="radio" name="feed-candidate" value={c.url} bind:group={chosen} disabled={c.subscribed} />
            <span class="info">
              <span class="name" dir="auto">{c.title || hostOf(c.url)}</span>
              <span class="url">{c.url}</span>
              <span class="stats">
                {$t('feeds.add.entries').replace('{n}', String(c.entryCount))}
                {#if c.lastPost}
                  · {$t('feeds.add.lastPost').replace('{when}', formatListDate(c.lastPost, timeFormat))}
                {/if}
                {#if c.subscribed}
                  · {$t('feeds.add.subscribed')}
                {/if}
              </span>
              {#if c.latest.length > 0}
                <ul class="latest">
                  {#each c.latest as line}
                    <li dir="auto">{line}</li>
                  {/each}
                </ul>
              {/if}
            </span>
          </label>
        {/each}
      </fieldset>

      <div class="fields">
        <label class="field">
          <span>{$t('feeds.add.name')}</span>
          <input class="cal-field" bind:value={title} placeholder={pick?.title ?? ''} />
        </label>
        <div class="field">
          <span>{$t('feeds.settings.folder')}</span>
          <Select value={folder} items={folderItems} ariaLabel={$t('feeds.settings.folder')} on:change={(e) => (folder = e.detail)} />
        </div>
      </div>
    {/if}
  </div>

  <svelte:fragment slot="footer">
    <div class="buttons">
      <button type="button" class="cal-btn" on:click={() => dispatch('close')}>{$t('feeds.cancel')}</button>
      <button type="button" class="cal-btn primary" disabled={!pick || pick.subscribed || adding} on:click={() => void subscribe()}>
        {#if adding}
          <PieProgress label={$t('feeds.add.adding')} />
        {/if}
        {$t('feeds.add.subscribe')}
      </button>
    </div>
  </svelte:fragment>
</Modal>

<style>
  .form {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .find {
    display: flex;
    gap: var(--space-2);
  }

  .address {
    flex: 1;
  }

  .error {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--danger);
  }

  .found {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin: 0;
    padding: 0;
    border: none;
  }

  legend {
    margin-bottom: var(--space-2);
    padding: 0;
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .candidate {
    display: flex;
    gap: var(--space-3);
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    cursor: var(--cursor-action);
    transition:
      border-color var(--motion-fast) var(--ease-out),
      background-color var(--motion-fast) var(--ease-out);
  }

  .candidate.on {
    border-color: var(--accent);
    background: var(--selection-bg);
  }

  .candidate.taken {
    cursor: default;
    opacity: 0.6;
  }

  .candidate input {
    margin: 2px 0 0;
    accent-color: var(--accent);
  }

  .info {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .name {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
  }

  .url {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    overflow-wrap: anywhere;
  }

  .stats {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .latest {
    margin: var(--space-1) 0 0;
    padding-inline-start: var(--space-4);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .latest li {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .fields {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(calc(var(--feed-dialog-width) / 3), 1fr));
    gap: var(--space-3);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .buttons {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    width: 100%;
  }

  @media (prefers-reduced-motion: reduce) {
    .candidate {
      transition: none;
    }
  }
</style>
