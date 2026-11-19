<script lang="ts">
  // the feeds step of the setup: a first feed by its address, or a whole list
  // from an OPML file. Find only asks the address typed; Subscribe stores it.
  import { createEventDispatcher } from 'svelte'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import fileIcon from '@phosphor-icons/core/assets/duotone/file-arrow-up-duotone.svg?raw'
  import checkIcon from '@phosphor-icons/core/assets/duotone/check-circle-duotone.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import '../../theme/controls.css'
  import { addFeed, discoverFeeds, importOpml } from '../../lib/api'
  import { checkFeedAddress } from '../../lib/setupplan'
  import { errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { FeedCandidate } from '../../lib/types'

  /** How many feeds there were before this step. */
  export let existing = 0

  const dispatch = createEventDispatcher<{ added: number }>()

  let address = ''
  let problem = ''
  let error = ''
  let finding = false
  let adding = false
  let importing = false
  let found: FeedCandidate[] = []
  let chosen = ''
  let added: string[] = []
  let imported = 0

  $: pick = found.find((c) => c.url === chosen) ?? null

  async function find(): Promise<void> {
    const check = checkFeedAddress(address)
    problem = check ? $t(check) : ''
    if (check || finding) {
      return
    }
    finding = true
    error = ''
    found = []
    try {
      found = await discoverFeeds(address.trim())
      chosen = found.find((c) => !c.subscribed)?.url ?? ''
      if (found.length === 0) {
        error = $t('setup.feeds.none')
      }
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
      const feed = await addFeed(pick.url, 0, '')
      added = [...added, feed.title || pick.title || pick.url]
      found = []
      address = ''
      dispatch('added', 1)
    } catch (err) {
      error = errorMessage(err)
    } finally {
      adding = false
    }
  }

  async function runImport(): Promise<void> {
    if (importing) {
      return
    }
    importing = true
    error = ''
    try {
      const r = await importOpml()
      if (r.added > 0) {
        imported += r.added
        dispatch('added', r.added)
      }
    } catch (err) {
      error = errorMessage(err)
    } finally {
      importing = false
    }
  }
</script>

{#if existing > 0 || added.length > 0 || imported > 0}
  <p class="state" role="status">
    <Ph svg={checkIcon} size={16} />
    <span>
      {$t('setup.feeds.have').replace('{n}', String(existing + added.length + imported))}
      {#if added.length > 0}<span class="names">{added.join(', ')}</span>{/if}
    </span>
  </p>
{/if}

<form class="find" on:submit|preventDefault={() => (pick && !pick.subscribed ? subscribe() : find())}>
  <label for="setup-feed">{$t('feeds.add.address')}</label>
  <div class="row">
    <input
      id="setup-feed"
      class="cal-field"
      bind:value={address}
      placeholder={$t('feeds.add.placeholder')}
      aria-invalid={problem !== ''}
      aria-describedby={problem ? 'feed-problem' : undefined}
      on:input={() => {
        problem = ''
        found = []
      }}
    />
    <button type="submit" class="cal-btn" disabled={finding || adding}>
      {#if finding}<PieProgress label={$t('feeds.add.finding')} />{/if}
      {$t('feeds.add.find')}
    </button>
  </div>
  {#if problem}
    <p class="error" id="feed-problem"><Ph svg={warningIcon} size={13} />{problem}</p>
  {/if}

  {#if found.length > 0}
    <div class="found" role="radiogroup" aria-label={$t('setup.feeds.found')}>
      {#each found as c (c.url)}
        <label class="candidate" class:off={c.subscribed}>
          <input type="radio" name="setup-feed-pick" value={c.url} bind:group={chosen} disabled={c.subscribed} />
          <span class="c-text">
            <span class="c-title">{c.title || c.url}</span>
            <span class="c-meta">{c.subscribed ? $t('feeds.add.subscribed') : $t('feeds.add.entries').replace('{n}', String(c.entryCount))}</span>
          </span>
        </label>
      {/each}
      <div>
        <button type="submit" class="cal-btn primary" disabled={!pick || pick.subscribed || adding}>
          {#if adding}<PieProgress label={$t('feeds.add.adding')} />{/if}
          {$t('feeds.add.subscribe')}
        </button>
      </div>
    </div>
  {/if}
</form>

{#if error}
  <p class="error" role="alert"><Ph svg={warningIcon} size={13} />{error}</p>
{/if}

<div class="or"><span>{$t('setup.or')}</span></div>

<button type="button" class="cal-btn" disabled={importing} on:click={runImport}>
  {#if importing}<PieProgress label={$t('setup.feeds.importing')} />{:else}<Ph svg={fileIcon} size={14} />{/if}
  {$t('setup.feeds.opml')}
</button>

<style>
  .find {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  label[for] {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .row {
    display: flex;
    gap: var(--space-2);
  }

  .row input {
    flex: 1;
    min-width: 0;
  }

  .found {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin-top: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  .candidate {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    cursor: var(--cursor-action);
  }

  .candidate.off {
    opacity: 0.6;
    cursor: default;
  }

  .candidate input {
    accent-color: var(--accent);
  }

  .c-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .c-title {
    font-size: var(--fz-list);
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  .c-meta {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .state,
  .error {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
    margin: 0 0 var(--space-4);
    font-size: var(--fz-label);
    line-height: 1.45;
    overflow-wrap: anywhere;
  }

  .state {
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .state :global(.ph) {
    color: var(--success);
  }

  .names {
    display: block;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .error {
    margin: var(--space-2) 0 0;
    color: var(--danger);
  }

  .or {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin: var(--space-5) 0;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  .or::before,
  .or::after {
    content: '';
    flex: 1;
    border-top: var(--hairline) solid var(--border-default);
  }
</style>
