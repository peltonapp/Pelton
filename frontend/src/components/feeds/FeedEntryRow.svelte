<script lang="ts">
  // one row of the entry list, drawn like a mail list row: the source and the
  // time on top, the title below, then the start of the text. unread shows by
  // weight and a dot; the star and read-later marks sit with the time, and the
  // star can be toggled from the row. a thumbnail shows only when the feed's
  // images may load.
  import { createEventDispatcher } from 'svelte'
  import starIcon from '@phosphor-icons/core/assets/fill/star-fill.svg?raw'
  import starOutline from '@phosphor-icons/core/assets/duotone/star-duotone.svg?raw'
  import laterIcon from '@phosphor-icons/core/assets/fill/bookmark-simple-fill.svg?raw'
  import audioIcon from '@phosphor-icons/core/assets/duotone/speaker-high-duotone.svg?raw'
  import videoIcon from '@phosphor-icons/core/assets/duotone/video-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import FeedIcon from './FeedIcon.svelte'
  import { formatListDate, type TimeFormat } from '../../lib/format'
  import { mediaKind, type FeedRow } from '../../lib/feedlogic'
  import { t } from '../../lib/i18n'

  export let row: FeedRow
  /** The data url of the feed's icon. */
  export let icon: string = ''
  export let selected: boolean = false
  /** Shows the entry's thumbnail; only when its feed may load images. */
  export let thumbnail: boolean = false
  /** Shows the source line; a single feed's list leaves it out. */
  export let showSource: boolean = true
  export let timeFormat: TimeFormat = 'auto'

  const dispatch = createEventDispatcher<{ star: void }>()

  $: media = row.media.map(mediaKind)
  $: hasAudio = media.includes('audio')
  $: hasVideo = media.includes('video')
  $: time = row.published ? formatListDate(row.published, timeFormat) : ''
</script>

<div
  class="row"
  class:unread={!row.read}
  class:selected
  role="option"
  tabindex="-1"
  aria-selected={selected}
  aria-label={`${row.read ? '' : `${$t('feeds.unread')}, `}${row.title || $t('feeds.untitled')}`}
  data-feed-row={row.key}
  on:click
  on:dblclick
  on:contextmenu
  on:keydown
>
  <div class="content">
    <div class="line top">
      {#if !row.read}
        <span class="dot" aria-hidden="true"></span>
      {/if}
      {#if showSource}
        <FeedIcon {icon} mail={row.mail} />
        <span class="source" dir="auto">{row.source}</span>
      {:else if row.author}
        <span class="source" dir="auto">{row.author}</span>
      {:else}
        <span class="source"></span>
      {/if}
      <span class="meta">
        {#if hasAudio}
          <span class="marker" title={$t('feeds.media.audio')}><Ph svg={audioIcon} size={12} /></span>
        {:else if hasVideo}
          <span class="marker" title={$t('feeds.media.video')}><Ph svg={videoIcon} size={12} /></span>
        {/if}
        {#if row.readLater}
          <span class="marker later" title={$t('feeds.readLater')}><Ph svg={laterIcon} size={12} /></span>
        {/if}
        {#if time}
          <time class="time" datetime={row.published}>{time}</time>
        {/if}
        <button
          type="button"
          class="star"
          class:on={row.starred}
          tabindex="-1"
          aria-pressed={row.starred}
          aria-label={row.starred ? $t('feeds.unstar') : $t('feeds.star')}
          title={row.starred ? $t('feeds.unstar') : $t('feeds.star')}
          on:click|stopPropagation={() => dispatch('star')}
          on:dblclick|stopPropagation
        >
          <Ph svg={row.starred ? starIcon : starOutline} size={13} />
        </button>
      </span>
    </div>
    <div class="body">
      <div class="text">
        <div class="title" dir="auto">{row.title || $t('feeds.untitled')}</div>
        {#if row.snippet}
          <div class="snippet" dir="auto">{row.snippet}</div>
        {/if}
      </div>
      {#if thumbnail && row.image}
        <img class="thumb" src={row.image} alt="" loading="lazy" referrerpolicy="no-referrer" draggable="false" />
      {/if}
    </div>
  </div>
</div>

<style>
  .row {
    position: relative;
    display: flex;
    padding: var(--row-pad-y) var(--row-pad-x);
    border-bottom: var(--hairline) solid var(--border-subtle);
    background: var(--surface-raised);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .row:hover {
    background: var(--surface-hover);
  }

  .row.selected {
    background: var(--selection-bg);
  }

  .row:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .content {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--row-gap);
  }

  .line.top {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
  }

  .dot {
    width: 7px;
    height: 7px;
    flex-shrink: 0;
    border-radius: 999px;
    background: var(--text-primary);
  }

  .source {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .meta {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    flex-shrink: 0;
    margin-inline-start: auto;
    color: var(--text-tertiary);
  }

  .marker {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  .marker.later {
    color: var(--accent);
  }

  .time {
    font-size: var(--fz-meta);
    font-variant-numeric: tabular-nums;
  }

  .star {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    opacity: 0;
    transition: opacity var(--motion-fast) var(--ease-out);
  }

  .row:hover .star,
  .row.selected .star,
  .star.on {
    opacity: 1;
  }

  .star.on {
    color: var(--warning);
  }

  .body {
    display: flex;
    gap: var(--space-3);
    min-width: 0;
  }

  .text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--row-gap);
  }

  .title {
    font-size: var(--fz-list);
    color: var(--text-secondary);
    overflow: hidden;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    line-height: var(--list-line-height);
  }

  .unread .title {
    color: var(--text-primary);
    font-weight: var(--fw-semibold);
  }

  .snippet {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    line-height: var(--list-line-height);
  }

  .thumb {
    flex-shrink: 0;
    width: var(--feed-thumb);
    height: var(--feed-thumb);
    object-fit: cover;
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }

  @media (prefers-reduced-motion: reduce) {
    .row,
    .star {
      transition: none;
    }
  }
</style>
