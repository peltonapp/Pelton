<script lang="ts">
  // the reading pane of the Feeds space: the open entry's title, source,
  // author and date, the toolbar (star, read later, read state, open in the
  // browser, copy the link, share by mail), the reader's text size and width,
  // then the entry through the mail body renderer, with remote images held
  // back like mail's. files the entry carries follow: audio and video with the
  // native controls, which load nothing until played, images once remote
  // content may load, and a download link for each. a newsletter mail opens
  // here too, as the mail it is.
  import { createEventDispatcher } from 'svelte'
  import starFill from '@phosphor-icons/core/assets/fill/star-fill.svg?raw'
  import starIcon from '@phosphor-icons/core/assets/duotone/star-duotone.svg?raw'
  import laterFill from '@phosphor-icons/core/assets/fill/bookmark-simple-fill.svg?raw'
  import laterIcon from '@phosphor-icons/core/assets/duotone/bookmark-simple-duotone.svg?raw'
  import readIcon from '@phosphor-icons/core/assets/duotone/envelope-open-duotone.svg?raw'
  import unreadIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
  import openIcon from '@phosphor-icons/core/assets/duotone/arrow-square-out-duotone.svg?raw'
  import linkIcon from '@phosphor-icons/core/assets/duotone/link-simple-duotone.svg?raw'
  import shareIcon from '@phosphor-icons/core/assets/duotone/paper-plane-tilt-duotone.svg?raw'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import downloadIcon from '@phosphor-icons/core/assets/duotone/download-simple-duotone.svg?raw'
  import narrowIcon from '@phosphor-icons/core/assets/duotone/arrows-in-line-horizontal-duotone.svg?raw'
  import mediumIcon from '@phosphor-icons/core/assets/duotone/text-align-justify-duotone.svg?raw'
  import fullIcon from '@phosphor-icons/core/assets/duotone/arrows-out-line-horizontal-duotone.svg?raw'
  import smallerIcon from '@phosphor-icons/core/assets/duotone/minus-duotone.svg?raw'
  import largerIcon from '@phosphor-icons/core/assets/duotone/plus-duotone.svg?raw'
  import bookIcon from '@phosphor-icons/core/assets/duotone/book-open-text-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import MailBody from '../detail/MailBody.svelte'
  import FeedIcon from './FeedIcon.svelte'
  import { getFeedEntryHtml, updateFeed } from '../../lib/api'
  import { openURL } from '../../lib/browser'
  import { fileName, hostOf, mediaKind, type FeedRow } from '../../lib/feedlogic'
  import { formatBytes, formatFullDate, type TimeFormat } from '../../lib/format'
  import { prefs, setMessageFontSize, setReaderWidth } from '../../stores/prefs'
  import { loadFeedSidebar } from '../../stores/feeds'
  import { t } from '../../lib/i18n'
  import type { BodyDetail, Feed, FeedEntryDetail, MessageDetail, ReaderWidth, RemoteSource } from '../../lib/types'

  /** The open row, or null for the empty pane. */
  export let row: FeedRow | null
  /** The entry behind a feed row, once loaded. */
  export let entry: FeedEntryDetail | null = null
  /** The message behind a newsletter row, once loaded. */
  export let mail: MessageDetail | null = null
  /** The feed the entry is from. */
  export let feed: Feed | undefined = undefined
  export let loading: boolean = false
  export let error: string = ''
  /** Shows a close button, for the pane laid over the list in a narrow window. */
  export let closable: boolean = false

  const dispatch = createEventDispatcher<{
    star: void
    later: void
    toggleRead: void
    openBrowser: void
    copyLink: void
    share: void
    close: void
    showFeed: number
  }>()

  const fontSizes = [12, 14, 16, 18, 20]
  $: fontIndex = Math.max(0, fontSizes.indexOf($prefs.messageFontSize))

  function stepFont(by: number): void {
    const next = fontSizes[Math.min(fontSizes.length - 1, Math.max(0, fontIndex + by))]
    if (next !== $prefs.messageFontSize) {
      setMessageFontSize(next)
    }
  }

  $: widths = [
    { key: 'narrow' as ReaderWidth, icon: narrowIcon, label: $t('reader.width.narrow') },
    { key: 'medium' as ReaderWidth, icon: mediumIcon, label: $t('reader.width.medium') },
    { key: 'full' as ReaderWidth, icon: fullIcon, label: $t('reader.width.full') },
  ]

  // the body rendered again with remote content, for the entry it was asked
  // for; and whether that happened, which also shows the lead image and the
  // image enclosures.
  let loadedHtml: { id: number; html: string } | null = null
  $: if (entry && loadedHtml && loadedHtml.id !== entry.id) {
    loadedHtml = null
  }
  $: imagesShown = !!entry && (entry.remoteAllowed || loadedHtml?.id === entry.id)

  $: bodyDetail = entry ? toBody(entry, loadedHtml) : null

  function toBody(e: FeedEntryDetail, loaded: { id: number; html: string } | null): BodyDetail {
    return {
      // negative, so nothing keyed by message id can mistake it for a mail.
      id: -e.id,
      date: e.published,
      fromName: feed?.title ?? '',
      fromAddress: '',
      isHtml: true,
      bodyHtmlSafe: loaded?.id === e.id ? loaded.html : e.bodyHtmlSafe,
      bodyPlain: '',
      hasRemoteContent: e.hasRemoteContent,
      remoteAllowed: e.remoteAllowed,
      remoteHosts: e.remoteHosts ?? [],
      trackingPixels: e.trackingPixels ?? [],
    }
  }

  $: remote = entry ? remoteFor(entry, feed) : null

  function remoteFor(e: FeedEntryDetail, f: Feed | undefined): RemoteSource {
    return {
      load: async (withPixels: boolean) => {
        const html = await getFeedEntryHtml(e.id, true, withPixels)
        loadedHtml = { id: e.id, html }
      },
      always: f
        ? {
            label: $t('feeds.images.always'),
            title: $t('feeds.images.alwaysTitle').replace('{name}', f.title),
            run: async () => {
              await updateFeed({ id: f.id, customTitle: f.customTitle, folderId: f.folderId, intervalMinutes: f.intervalMinutes, loadImages: true, notify: f.notify })
              await loadFeedSidebar()
            },
          }
        : undefined,
    }
  }

  // a newsletter's remote content, once loaded, is kept with its own copy here.
  let mailHtml: { id: number; html: string } | null = null
  $: shownMail = mail && mailHtml?.id === mail.id ? { ...mail, bodyHtmlSafe: mailHtml.html } : mail

  // the lead image, unless the text already shows it.
  $: hero = entry && imagesShown && entry.image && !(bodyDetail?.bodyHtmlSafe ?? '').includes(entry.image) ? entry.image : ''
  $: media = entry ? entry.media.map((m) => ({ ...m, kind: mediaKind(m) })) : []
  $: timeFormat = $prefs.timeFormat as TimeFormat
  $: emptyBody = !!entry && !(entry.bodyHtmlSafe ?? '').trim() && media.length === 0
</script>

<section class="reader" aria-label={$t('feeds.reader')}>
  {#if !row}
    <EmptyState icon={bookIcon} title={$t('feeds.reader.empty')} detail={$t('feeds.reader.emptyDetail')} />
  {:else}
    <div class="scroll selectable" tabindex="-1" data-feed-reader>
      <div class="column" data-width={$prefs.readerWidth}>
        <header class="head">
          <div class="source">
            {#if row.mail}
              <FeedIcon mail />
              <span class="feed-name" dir="auto">{row.source}</span>
            {:else if feed}
              <FeedIcon icon={feed.icon} />
              <button type="button" class="feed-name link" dir="auto" on:click={() => dispatch('showFeed', feed?.id ?? 0)}>{feed.title}</button>
            {/if}
            <span class="spacer"></span>
            {#if closable}
              <button type="button" class="tool" aria-label={$t('feeds.reader.close')} title={$t('feeds.reader.close')} on:click={() => dispatch('close')}>
                <Ph svg={closeIcon} size={14} />
              </button>
            {/if}
          </div>
          <h1 dir="auto">
            {#if row.url}
              <a href={row.url} on:click|preventDefault={() => dispatch('openBrowser')}>{row.title || $t('feeds.untitled')}</a>
            {:else}
              {row.title || $t('feeds.untitled')}
            {/if}
          </h1>
          <p class="byline">
            {#if row.author && !row.mail}
              <span dir="auto">{row.author}</span>
              <span aria-hidden="true">·</span>
            {/if}
            {#if row.published}
              <time datetime={row.published}>{formatFullDate(row.published, timeFormat)}</time>
            {/if}
            {#if row.url}
              <span aria-hidden="true">·</span>
              <span class="host">{hostOf(row.url)}</span>
            {/if}
          </p>
          <div class="toolbar">
            <div class="actions" role="toolbar" aria-label={$t('feeds.reader.actions')}>
              <button type="button" class="tool" aria-pressed={row.starred} aria-label={row.starred ? $t('feeds.unstar') : $t('feeds.star')} title={row.starred ? $t('feeds.unstar') : $t('feeds.star')} on:click={() => dispatch('star')}>
                <Ph svg={row.starred ? starFill : starIcon} size={15} />
              </button>
              {#if !row.mail}
                <button type="button" class="tool" aria-pressed={row.readLater} aria-label={row.readLater ? $t('feeds.readLater.remove') : $t('feeds.readLater.add')} title={row.readLater ? $t('feeds.readLater.remove') : $t('feeds.readLater.add')} on:click={() => dispatch('later')}>
                  <Ph svg={row.readLater ? laterFill : laterIcon} size={15} />
                </button>
              {/if}
              <button type="button" class="tool" aria-label={row.read ? $t('feeds.markUnread') : $t('feeds.markRead')} title={row.read ? $t('feeds.markUnread') : $t('feeds.markRead')} on:click={() => dispatch('toggleRead')}>
                <Ph svg={row.read ? unreadIcon : readIcon} size={15} />
              </button>
              {#if row.url}
                <span class="sep" aria-hidden="true"></span>
                <button type="button" class="tool" aria-label={$t('feeds.openBrowser')} title={$t('feeds.openBrowser')} on:click={() => dispatch('openBrowser')}>
                  <Ph svg={openIcon} size={15} />
                </button>
                <button type="button" class="tool" aria-label={$t('feeds.copyLink')} title={$t('feeds.copyLink')} on:click={() => dispatch('copyLink')}>
                  <Ph svg={linkIcon} size={15} />
                </button>
                <button type="button" class="tool" aria-label={$t('feeds.share')} title={$t('feeds.share')} on:click={() => dispatch('share')}>
                  <Ph svg={shareIcon} size={15} />
                </button>
              {/if}
            </div>
            <div class="view" role="group" aria-label={$t('reader.view')}>
              <button type="button" class="tool" aria-label={$t('reader.text.smaller')} title={$t('reader.text.smaller')} disabled={fontIndex === 0} on:click={() => stepFont(-1)}>
                <Ph svg={smallerIcon} size={14} />
              </button>
              <span class="aa" aria-hidden="true">Aa</span>
              <button type="button" class="tool" aria-label={$t('reader.text.larger')} title={$t('reader.text.larger')} disabled={fontIndex === fontSizes.length - 1} on:click={() => stepFont(1)}>
                <Ph svg={largerIcon} size={14} />
              </button>
              <span class="sep" aria-hidden="true"></span>
              <div class="widths" role="radiogroup" aria-label={$t('reader.width')}>
                {#each widths as w (w.key)}
                  <button type="button" class="tool" role="radio" aria-checked={$prefs.readerWidth === w.key} aria-label={w.label} title={w.label} on:click={() => setReaderWidth(w.key)}>
                    <Ph svg={w.icon} size={14} />
                  </button>
                {/each}
              </div>
            </div>
          </div>
        </header>

        {#if error}
          <p class="error" role="alert">{error}</p>
        {:else if loading && !entry && !mail}
          <div class="loading"><PieProgress label={$t('feeds.loading')} /></div>
        {:else if row.mail && shownMail}
          <div class="body">
            <MailBody detail={shownMail} on:html={(e) => (mailHtml = e.detail)} />
          </div>
        {:else if entry && bodyDetail}
          {#if hero}
            <img class="hero" src={hero} alt="" referrerpolicy="no-referrer" />
          {/if}
          {#if emptyBody}
            <p class="no-text">{$t('feeds.reader.noText')}</p>
          {:else if entry.bodyHtmlSafe}
            <div class="body">
              <MailBody detail={bodyDetail} {remote} />
            </div>
          {/if}
          {#if media.length > 0}
            <ul class="media" aria-label={$t('feeds.media')}>
              {#each media as m (m.url)}
                <li class="file">
                  {#if m.kind === 'audio'}
                    <!-- svelte-ignore a11y-media-has-caption -->
                    <audio controls preload="none" src={m.url}></audio>
                  {:else if m.kind === 'video'}
                    <!-- svelte-ignore a11y-media-has-caption -->
                    <video controls preload="none" src={m.url}></video>
                  {:else if m.kind === 'image' && imagesShown}
                    <img src={m.url} alt={m.title} referrerpolicy="no-referrer" />
                  {/if}
                  <div class="file-line">
                    <span class="file-name" dir="auto">{m.title || fileName(m.url)}</span>
                    {#if m.length > 0}
                      <span class="file-size">{formatBytes(m.length)}</span>
                    {/if}
                    <a class="download" href={m.url} on:click|preventDefault={() => openURL(m.url)}>
                      <Ph svg={downloadIcon} size={14} />
                      {$t('feeds.download')}
                    </a>
                  </div>
                </li>
              {/each}
            </ul>
          {/if}
        {/if}
      </div>
    </div>
  {/if}
</section>

<style>
  .reader {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
    height: 100%;
    background: var(--surface-base);
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  .scroll:focus {
    outline: none;
  }

  .column {
    width: 100%;
    margin-inline: auto;
    padding: var(--pane-pad) var(--pane-pad) calc(var(--pane-pad) * 3);
  }

  .column[data-width='narrow'] {
    max-width: var(--reader-width-narrow);
  }

  .column[data-width='medium'] {
    max-width: var(--reader-width-medium);
  }

  .head {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin-bottom: var(--space-4);
    padding-bottom: var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .source {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
  }

  .feed-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .feed-name.link {
    padding: 0;
    border: none;
    background: transparent;
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .feed-name.link:hover {
    color: var(--text-primary);
    text-decoration: underline;
  }

  .spacer {
    flex: 1;
  }

  h1 {
    margin: 0;
    font-size: var(--fz-title);
    font-weight: var(--fw-semibold);
    line-height: 1.3;
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  h1 a {
    color: inherit;
    text-decoration: none;
  }

  h1 a:hover {
    text-decoration: underline;
  }

  .byline {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .host {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
  }

  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
  }

  .actions,
  .view {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    flex-shrink: 0;
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
  }

  .widths {
    display: inline-flex;
    gap: var(--space-1);
  }

  .tool {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .tool:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .tool:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -1px;
  }

  .tool:disabled {
    cursor: default;
    opacity: 0.4;
  }

  .tool[aria-pressed='true'],
  .tool[aria-checked='true'] {
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .aa {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .sep {
    width: var(--hairline);
    align-self: stretch;
    margin-inline: var(--space-1);
    background: var(--border-default);
  }

  .body {
    display: flex;
    flex-direction: column;
    position: relative;
  }

  .hero {
    display: block;
    max-width: 100%;
    height: auto;
    margin-bottom: var(--space-4);
    border-radius: var(--radius-card);
  }

  .no-text,
  .error {
    margin: 0;
    font-size: var(--fz-list);
    color: var(--text-tertiary);
  }

  .error {
    color: var(--danger);
  }

  .loading {
    display: flex;
    justify-content: center;
    padding: var(--space-6);
    color: var(--text-tertiary);
  }

  .media {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    margin: var(--space-5) 0 0;
    padding: 0;
    list-style: none;
  }

  .file {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  .file audio,
  .file video,
  .file img {
    width: 100%;
    max-width: 100%;
    border-radius: var(--radius-control);
  }

  .file-line {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
    font-size: var(--fz-label);
  }

  .file-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-primary);
  }

  .file-size {
    flex-shrink: 0;
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .download {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    flex-shrink: 0;
    color: var(--link);
    text-decoration: none;
  }

  .download:hover {
    text-decoration: underline;
  }

  @media (prefers-reduced-motion: reduce) {
    .tool {
      transition: none;
    }
  }
</style>
