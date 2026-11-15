<script lang="ts">
  // the newsletter manager: every sender whose mail offers List-Unsubscribe,
  // with how much and how often they write, and the actions to be rid of them.
  // It takes the place of the message list and the reading pane; Escape or
  // the close button brings those back.
  import { createEventDispatcher, onMount } from 'svelte'
  import { get } from 'svelte/store'
  import newspaperIcon from '@phosphor-icons/core/assets/duotone/newspaper-duotone.svg?raw'
  import bellSlashIcon from '@phosphor-icons/core/assets/duotone/bell-slash-duotone.svg?raw'
  import archiveIcon from '@phosphor-icons/core/assets/duotone/archive-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import broomIcon from '@phosphor-icons/core/assets/duotone/broom-duotone.svg?raw'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
  import refreshIcon from '@phosphor-icons/core/assets/duotone/arrows-clockwise-duotone.svg?raw'
  import funnelIcon from '@phosphor-icons/core/assets/duotone/funnel-simple-duotone.svg?raw'
  import checkIcon from '@phosphor-icons/core/assets/duotone/check-circle-duotone.svg?raw'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import Segmented from '../settings2/ui/Segmented.svelte'
  import { t } from '../../lib/i18n'
  import { tip } from '../../lib/tip'
  import { formatBytes, formatListDate, type TimeFormat } from '../../lib/format'
  import { popupIsOpen } from '../../lib/popups'
  import {
    filterNewsletters,
    frequencyLabel,
    newsletterSorts,
    senderLabel,
    sortNewsletters,
    type NewsletterSender,
    type NewsletterSort,
  } from '../../lib/mailstats'
  import {
    archiveSenders,
    deleteOlderThan,
    keepLatestOnly,
    loadNewsletters,
    newsletterProgress,
    newsletters,
    showSenderMessages,
    unsubscribeSenders,
  } from '../../stores/newsletters'
  import { contextMenu, openContextMenu } from '../../stores/contextmenu'
  import { confirmRequest } from '../../stores/confirm'
  import { prefs } from '../../stores/prefs'

  const dispatch = createEventDispatcher<{ close: void }>()

  /** The ages "delete older than" offers, in days. */
  const ages = [30, 90, 180, 365]

  let sort: NewsletterSort = 'volume'
  let filter = ''
  // selected senders by address, so a reload keeps the selection.
  let picked = new Set<string>()

  onMount(() => {
    void loadNewsletters()
  })

  $: shown = sortNewsletters(filterNewsletters($newsletters.senders, filter), sort)
  $: picked = new Set([...picked].filter((e) => $newsletters.senders.some((s) => s.email === e)))
  $: selected = shown.filter((s) => picked.has(s.email))
  $: allPicked = shown.length > 0 && selected.length === shown.length
  $: sortOptions = newsletterSorts.map((key) => ({ key, label: $t(`newsletters.sort.${key}`) }))
  $: busy = $newsletterProgress !== null

  function toggle(email: string): void {
    const next = new Set(picked)
    if (next.has(email)) {
      next.delete(email)
    } else {
      next.add(email)
    }
    picked = next
  }

  function toggleAll(): void {
    picked = allPicked ? new Set() : new Set(shown.map((s) => s.email))
  }

  function ageLabel(days: number): string {
    return $t(`newsletters.age.${days}`)
  }

  function openAgeMenu(event: MouseEvent, senders: NewsletterSender[]): void {
    const rect = (event.currentTarget as HTMLElement).getBoundingClientRect()
    openContextMenu(
      rect.left,
      rect.bottom + 4,
      ages.map((days) => ({ label: ageLabel(days), danger: true, action: () => void run(() => deleteOlderThan(senders, days)) })),
    )
  }

  async function run(action: () => Promise<void>): Promise<void> {
    if (busy) {
      return
    }
    await action()
  }

  function kindHint(s: NewsletterSender): string {
    return s.unsubscribeKind === '' ? '' : $t(`newsletters.unsubscribe.how.${s.unsubscribeKind}`)
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key !== 'Escape' || event.defaultPrevented) {
      return
    }
    if (popupIsOpen() || get(contextMenu) !== null || get(confirmRequest) !== null) {
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

<section class="manager" aria-labelledby="newsletters-title">
  <header class="head">
    <div class="title">
      <Ph svg={newspaperIcon} size={18} />
      <h2 id="newsletters-title">{$t('newsletters.title')}</h2>
      {#if !$newsletters.loading || $newsletters.senders.length > 0}
        <span class="count">{$t('newsletters.senderCount').replace('{n}', String($newsletters.senders.length))}</span>
      {/if}
      {#if busy && $newsletterProgress}
        <span class="progress">
          <PieProgress
            value={$newsletterProgress.total > 0 ? $newsletterProgress.done / $newsletterProgress.total : null}
            label={$t('newsletters.working')}
          />
        </span>
      {/if}
    </div>
    <div class="tools">
      <button
        type="button"
        class="icon"
        aria-label={$t('newsletters.refresh')}
        use:tip={{ text: $t('newsletters.refresh') }}
        on:click={() => void loadNewsletters()}
      >
        <Ph svg={refreshIcon} size={15} />
      </button>
      <button
        type="button"
        class="icon"
        aria-label={$t('action.close')}
        use:tip={{ text: $t('action.close'), shortcut: 'Esc' }}
        on:click={() => dispatch('close')}
      >
        <Ph svg={xIcon} size={15} />
      </button>
    </div>
  </header>

  <div class="bar">
    <label class="filter">
      <Ph svg={funnelIcon} size={13} />
      <input
        type="text"
        autocomplete="off"
        spellcheck="false"
        placeholder={$t('newsletters.filter')}
        aria-label={$t('newsletters.filter')}
        bind:value={filter}
      />
    </label>
    <Segmented value={sort} options={sortOptions} label={$t('newsletters.sortBy')} on:change={(e) => (sort = e.detail as NewsletterSort)} />
  </div>

  {#if selected.length > 0}
    <div class="bulk" role="toolbar" aria-label={$t('newsletters.bulk')}>
      <span class="picked">{$t('newsletters.selected').replace('{n}', String(selected.length))}</span>
      <button type="button" class="act" disabled={busy} on:click={() => void run(() => unsubscribeSenders(selected))}>
        <Ph svg={bellSlashIcon} size={14} />{$t('newsletters.action.unsubscribe')}
      </button>
      <button type="button" class="act" disabled={busy} on:click={() => void run(() => archiveSenders(selected))}>
        <Ph svg={archiveIcon} size={14} />{$t('newsletters.action.archive')}
      </button>
      <button type="button" class="act" disabled={busy} aria-haspopup="menu" on:click={(e) => openAgeMenu(e, selected)}>
        <Ph svg={trashIcon} size={14} />{$t('newsletters.action.deleteOlder')}
      </button>
      <button type="button" class="act" disabled={busy} on:click={() => void run(() => keepLatestOnly(selected))}>
        <Ph svg={broomIcon} size={14} />{$t('newsletters.action.keepLatest')}
      </button>
      <button type="button" class="act quiet" on:click={() => (picked = new Set())}>{$t('newsletters.clearSelection')}</button>
    </div>
  {/if}

  <div class="scroll">
    {#if $newsletters.error}
      <p class="state error" role="alert">{$newsletters.error}</p>
    {:else if $newsletters.loading && $newsletters.senders.length === 0}
      <div class="state"><PieProgress size={18} label={$t('newsletters.loading')} /><span>{$t('newsletters.loading')}</span></div>
    {:else if $newsletters.senders.length === 0}
      <div class="state empty">
        <Ph svg={newspaperIcon} size={28} />
        <p>{$t('newsletters.empty')}</p>
      </div>
    {:else if shown.length === 0}
      <p class="state">{$t('newsletters.noMatch')}</p>
    {:else}
      <table>
        <thead>
          <tr>
            <th class="pick">
              <input type="checkbox" checked={allPicked} aria-label={$t('newsletters.selectAll')} on:change={toggleAll} />
            </th>
            <th class="sender">{$t('newsletters.col.sender')}</th>
            <th>{$t('newsletters.col.frequency')}</th>
            <th>{$t('newsletters.col.last')}</th>
            <th class="num">{$t('newsletters.col.messages')}</th>
            <th class="num">{$t('newsletters.col.unread')}</th>
            <th class="num">{$t('newsletters.col.size')}</th>
            <th class="actions"><span class="sr">{$t('newsletters.col.actions')}</span></th>
          </tr>
        </thead>
        <tbody>
          {#each shown as s (s.email)}
            <tr class:picked={picked.has(s.email)}>
              <td class="pick">
                <input
                  type="checkbox"
                  checked={picked.has(s.email)}
                  aria-label={$t('newsletters.select').replace('{name}', senderLabel(s))}
                  on:change={() => toggle(s.email)}
                />
              </td>
              <td class="sender">
                <span class="name">{senderLabel(s)}</span>
                <span class="sub">
                  {#if s.name}{s.email}{/if}
                  {#if s.listId}<span class="list">{s.listId}</span>{/if}
                </span>
                {#if s.unsubscribedAt}
                  <span class="done" use:tip={{ text: $t('newsletters.unsubscribedOn').replace('{date}', formatListDate(s.unsubscribedAt, $prefs.timeFormat as TimeFormat)) }}>
                    <Ph svg={checkIcon} size={12} />{$t('newsletters.unsubscribed')}
                  </span>
                {/if}
              </td>
              <td>{frequencyLabel(s.perWeek, $t)}</td>
              <td>{formatListDate(s.lastDate, $prefs.timeFormat as TimeFormat)}</td>
              <td class="num">{s.count}</td>
              <td class="num" class:has={s.unread > 0}>{s.unread}</td>
              <td class="num">{formatBytes(s.bytes)}</td>
              <td class="actions">
                <div class="row-acts">
                  <button
                    type="button"
                    class="icon"
                    disabled={busy || s.unsubscribeKind === '' || s.unsubscribedAt !== ''}
                    aria-label={$t('newsletters.action.unsubscribeFrom').replace('{name}', senderLabel(s))}
                    use:tip={{ text: s.unsubscribedAt ? $t('newsletters.unsubscribed') : `${$t('newsletters.action.unsubscribe')}${kindHint(s) ? `. ${kindHint(s)}` : ''}` }}
                    on:click={() => void run(() => unsubscribeSenders([s]))}
                  >
                    <Ph svg={bellSlashIcon} size={15} />
                  </button>
                  <button
                    type="button"
                    class="icon"
                    disabled={busy}
                    aria-label={$t('newsletters.action.archiveFrom').replace('{name}', senderLabel(s))}
                    use:tip={{ text: $t('newsletters.action.archive') }}
                    on:click={() => void run(() => archiveSenders([s]))}
                  >
                    <Ph svg={archiveIcon} size={15} />
                  </button>
                  <button
                    type="button"
                    class="icon"
                    disabled={busy}
                    aria-haspopup="menu"
                    aria-label={$t('newsletters.action.deleteOlderFrom').replace('{name}', senderLabel(s))}
                    use:tip={{ text: $t('newsletters.action.deleteOlder') }}
                    on:click={(e) => openAgeMenu(e, [s])}
                  >
                    <Ph svg={trashIcon} size={15} />
                  </button>
                  <button
                    type="button"
                    class="icon"
                    disabled={busy || s.count < 2}
                    aria-label={$t('newsletters.action.keepLatestFrom').replace('{name}', senderLabel(s))}
                    use:tip={{ text: $t('newsletters.action.keepLatest') }}
                    on:click={() => void run(() => keepLatestOnly([s]))}
                  >
                    <Ph svg={broomIcon} size={15} />
                  </button>
                  <button
                    type="button"
                    class="icon"
                    aria-label={$t('newsletters.action.showFrom').replace('{name}', senderLabel(s))}
                    use:tip={{ text: $t('newsletters.action.show') }}
                    on:click={() => showSenderMessages(s)}
                  >
                    <Ph svg={searchIcon} size={15} />
                  </button>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</section>

<style>
  .manager {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    height: 100%;
    background: var(--surface-base);
    color: var(--text-primary);
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
    gap: var(--space-3);
    min-width: 0;
    color: var(--text-secondary);
  }

  h2 {
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .count {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .progress {
    display: inline-flex;
    color: var(--text-secondary);
  }

  .tools,
  .row-acts {
    display: flex;
    align-items: center;
    gap: var(--space-1);
  }

  .icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .icon:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .icon:disabled {
    opacity: 0.35;
    cursor: default;
  }

  .bar,
  .bulk {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .bulk {
    background: var(--surface-raised);
    animation: open var(--motion-base) var(--ease-out);
  }

  @keyframes open {
    from {
      opacity: 0;
      transform: translateY(calc(var(--space-2) * -1));
    }
  }

  .filter {
    flex: 1;
    min-width: calc(var(--space-6) * 6);
    display: flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-tertiary);
  }

  .filter:focus-within {
    border-color: var(--accent);
  }

  .filter input {
    flex: 1;
    min-width: 0;
    padding: 0;
    border: none;
    outline: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
  }

  .picked {
    margin-inline-end: var(--space-2);
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
  }

  .act {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-base);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .act:hover:not(:disabled) {
    background: var(--surface-hover);
  }

  .act:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .act.quiet {
    border-color: transparent;
    background: transparent;
    color: var(--text-secondary);
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow: auto;
  }

  .state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    margin: 0;
    padding: var(--space-6);
    color: var(--text-tertiary);
    font-size: var(--fz-label);
  }

  .state.empty {
    flex-direction: column;
  }

  .state.error {
    color: var(--danger);
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--fz-list);
  }

  th {
    position: sticky;
    top: 0;
    z-index: 1;
    padding: var(--space-2) var(--space-3);
    border-bottom: var(--hairline) solid var(--border-default);
    background: var(--surface-base);
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    text-align: start;
    white-space: nowrap;
  }

  td {
    padding: var(--row-pad-y) var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
    color: var(--text-secondary);
    white-space: nowrap;
    vertical-align: middle;
  }

  tr:hover td {
    background: var(--surface-hover);
  }

  tr.picked td {
    background: var(--selection-bg);
  }

  .pick {
    width: 1%;
    padding-inline-end: 0;
  }

  .pick input {
    accent-color: var(--accent);
  }

  .sender {
    width: 40%;
    max-width: 0;
    white-space: normal;
  }

  .name {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-primary);
    font-weight: var(--fw-medium);
  }

  .sub {
    display: flex;
    gap: var(--space-3);
    overflow: hidden;
    white-space: nowrap;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .list {
    overflow: hidden;
    text-overflow: ellipsis;
    font-family: var(--font-mono);
  }

  .done {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    margin-top: var(--space-1);
    padding: 0 var(--space-2);
    border-radius: var(--radius-control);
    background: var(--success-bg);
    color: var(--success);
    font-size: var(--fz-meta);
  }

  .num {
    text-align: end;
    font-variant-numeric: tabular-nums;
  }

  td.has {
    color: var(--text-primary);
    font-weight: var(--fw-semibold);
  }

  .actions {
    width: 1%;
  }

  .row-acts {
    justify-content: flex-end;
    opacity: 0.55;
    transition: opacity var(--motion-fast) var(--ease-out);
  }

  tr:hover .row-acts,
  tr:focus-within .row-acts {
    opacity: 1;
  }

  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
</style>
