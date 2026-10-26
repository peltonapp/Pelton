<script lang="ts">
  // one row in the message list. its layout follows the chosen row template
  // (relaxed, comfortable, compact, single) with per-field overrides for the
  // avatar and snippet and a clamped preview-line count. unread shows by weight
  // and a dot (never accent). flagged rows highlight per the user's chosen style
  // (flag icon, a left bar, both icon+bar, or off); a color label paints the left
  // bar and a small dot. offline and snoozed states show little meta icons.
  // trackpad swipes (horizontal wheel) reveal a configurable action.
  import { createEventDispatcher } from 'svelte'
  import {
    IconStarFilled,
    IconPaperclip,
    IconFlagFilled,
    IconFlag,
    IconTrash,
    IconMailOpened,
    IconMailFilled,
    IconArchive,
    IconClock,
    IconDownload,
    IconClockPause,
  } from '@tabler/icons-svelte'
  import Avatar from '../common/Avatar.svelte'
  import { prefs } from '../../stores/prefs'
  import { formatListDate, displayName, type TimeFormat } from '../../lib/format'
  import { flagColorHex } from '../../theme/flagcolors'
  import { viewColorCss } from '../../lib/viewicons'
  import type { MessageSummary, RowTemplate, SwipeAction } from '../../lib/types'
  import { t } from '../../lib/i18n'
  import { vipSenders, bareAddress } from '../../stores/vip'

  export let message: MessageSummary
  export let selected: boolean = false
  // checked reflects membership in the bulk-selection set (modifier-click).
  export let checked: boolean = false
  // accountColor marks which mailbox the message is from in a list that mixes
  // them (#486); '' shows nothing.
  export let accountColor: string = ''

  const dispatch = createEventDispatcher<{ swipe: 'left' | 'right' }>()

  $: unread = !message.seen
  $: template = $prefs.rowTemplate as RowTemplate

  $: showAvatar = $prefs.rowShowAvatar && template !== 'single'
  $: showSnippet =
    $prefs.rowShowSnippet && (template === 'relaxed' || template === 'comfortable') && !!message.snippet
  $: avatarSize = template === 'relaxed' ? 36 : 30
  $: previewLines = $prefs.previewLines

  // flag highlight styles. "both" shows the flag icon and a left edge bar.
  $: flagStyle = $prefs.flagHighlight
  // the vip store (loaded at startup, updated on every toggle) is the single
  // source of truth for the star, so marking and un-marking both reflect at
  // once. The backend senderVip flag only seeds the store; OR-ing it in here
  // would keep a removed sender starred.
  $: isVip = $vipSenders.has(bareAddress(message.fromAddress))
  $: showFlagIcon = message.flagged && (flagStyle === 'flag' || flagStyle === 'both')
  $: barLeft = message.flagged && (flagStyle === 'left' || flagStyle === 'both')

  // a color label paints the left bar (taking precedence over the warning bar)
  // and a small dot in the meta row.
  $: colorHex = flagColorHex(message.flagColor)
  $: showBar = !!colorHex || barLeft
  $: barColor = colorHex || 'var(--warning)'
  $: snoozed = !!message.snoozeUntil
  $: showOffline = message.offline && $prefs.showOfflineIndicator

  // swipe gesture state. horizontal wheel deltas (trackpad) accumulate into a
  // translation that reveals the action behind the row, apple-mail style: the
  // reveal follows the swipe, resists past the row width, and the action only
  // fires once you have deliberately dragged past the commit line and let go.
  // "letting go" on a trackpad is inferred from wheel events stopping, so the
  // idle window is generous enough that a natural mid-swipe pause never counts
  // as a release. requiring a coherent horizontal burst to engage stops a
  // vertical scroll from ever being hijacked into a swipe.
  let wrapWidth = 320
  let offset = 0
  let settling = false
  let engaged = false
  let committed = false
  // pre-engage accumulation: we only start a swipe once there is a coherent burst
  // of horizontal movement, so ordinary vertical scrolling is never hijacked.
  let preDX = 0
  let idleTimer: ReturnType<typeof setTimeout> | undefined

  // must engage with ~28px of coherent horizontal movement, and commit only past
  // 60% of the row so a half-hearted nudge never fires. the reveal resists
  // (rubber-bands) past the full row width so the swipe feels anchored rather
  // than flinging off-screen.
  const engageThreshold = 28
  $: commitDistance = wrapWidth * 0.6
  $: maxReveal = wrapWidth

  $: leftAction = $prefs.swipeLeftAction as SwipeAction
  $: rightAction = $prefs.swipeRightAction as SwipeAction

  $: actionMeta = {
    none: { label: '', color: 'var(--surface-hover)', icon: IconFlag },
    delete: { label: $t('action.delete'), color: 'var(--danger)', icon: IconTrash },
    read: { label: $t('messageList.swipe.read'), color: 'var(--accent)', icon: IconMailOpened },
    unread: { label: $t('messageList.swipe.unread'), color: 'var(--accent)', icon: IconMailFilled },
    flag: { label: $t('messageList.flag'), color: 'var(--warning)', icon: IconFlagFilled },
    archive: { label: $t('action.archive'), color: 'var(--text-tertiary)', icon: IconArchive },
    snooze: { label: $t('shortcut.snooze'), color: 'var(--accent)', icon: IconClockPause },
  } as Record<SwipeAction, { label: string; color: string; icon: typeof IconTrash }>

  // revealed is the action about to fire given the current drag direction. a
  // positive offset (row dragged right) reveals the right-swipe action on the
  // left edge; negative reveals the left-swipe action on the right edge.
  $: revealAction = offset > 0 ? rightAction : leftAction
  $: revealMeta = actionMeta[revealAction] ?? actionMeta.none

  function onWheel(event: WheelEvent): void {
    if (!$prefs.swipeEnabled || (leftAction === 'none' && rightAction === 'none') || committed) {
      return
    }
    const dx = event.deltaX
    const dy = event.deltaY
    if (!engaged) {
      // a clearly vertical event: let it scroll and reset any partial buffer, so
      // stray horizontal jitter during scrolling never accumulates into a swipe.
      if (Math.abs(dy) > Math.abs(dx) * 1.2) {
        preDX = 0
        return
      }
      // accumulate horizontal intent; engage only after a deliberate burst.
      preDX += dx
      if (Math.abs(preDX) < engageThreshold) {
        return
      }
      engaged = true
      settling = false
      offset = 0
    }
    // once engaged, a clearly vertical burst means the user switched to
    // scrolling: end the gesture (snap back) rather than staying stuck open.
    if (Math.abs(dy) > Math.abs(dx) * 2.5) {
      scheduleEnd(0)
      return
    }
    event.preventDefault()
    settling = false
    offset = rubberBand(offset - dx, maxReveal)
    scheduleEnd(200)
  }

  // scheduleEnd (re)arms the release timer. a trackpad wheel has no end event, so
  // the gesture is considered released once deltas stop arriving for a moment.
  function scheduleEnd(delay: number): void {
    if (idleTimer) {
      clearTimeout(idleTimer)
    }
    idleTimer = setTimeout(endSwipe, delay)
  }

  function endSwipe(): void {
    if (!engaged || committed) {
      return
    }
    const dir = offset > 0 ? 'right' : 'left'
    const action = dir === 'right' ? rightAction : leftAction
    const passed = Math.abs(offset) >= commitDistance
    engaged = false
    preDX = 0
    settling = true
    if (passed && action !== 'none') {
      // slide the rest of the way so the whole row shows the action, then fire.
      committed = true
      offset = dir === 'right' ? maxReveal : -maxReveal
      setTimeout(() => {
        dispatch('swipe', dir)
        offset = 0
        committed = false
      }, 160)
    } else {
      offset = 0
    }
  }

  // rubberBand keeps the drag 1:1 within the row and heavily resists beyond it,
  // so the reveal feels anchored instead of sliding the row clean off-screen.
  function rubberBand(v: number, max: number): number {
    if (Math.abs(v) <= max) {
      return v
    }
    const over = Math.abs(v) - max
    return Math.sign(v) * (max + over * 0.25)
  }
</script>

<!-- keyboard navigation for the list is handled by the listbox container
     (arrow keys + enter), so the row itself only needs the click affordance. -->
<div class="swipe-wrap" class:swiping={offset !== 0} bind:clientWidth={wrapWidth}>
  {#if offset !== 0}
    <div
      class="reveal"
      class:right-edge={offset < 0}
      style={`--reveal-color:${revealMeta.color}`}
      aria-hidden="true"
    >
      <svelte:component this={revealMeta.icon} size={18} stroke={1.7} />
      <span>{revealMeta.label}</span>
    </div>
  {/if}
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-noninteractive-element-interactions -->
  <div
    class="row {template}"
    class:selected
    class:unread
    class:checked
    class:bar-left={showBar}
    class:account-mark={accountColor !== ''}
    class:settling
    style={`transform:translateX(${offset}px);--bar-color:${barColor};--account-color:${viewColorCss(accountColor)}`}
    role="option"
    tabindex="-1"
    aria-selected={selected}
    on:click
    on:dblclick
    on:auxclick
    on:contextmenu
    on:wheel={onWheel}
  >
    {#if showAvatar}
      <Avatar name={message.fromName} email={message.fromAddress} size={avatarSize} />
    {/if}

    <div class="content">
      <div class="line top">
        {#if unread}
          <span class="dot" aria-hidden="true"></span>
        {/if}
        {#if isVip}
          <IconStarFilled size={12} class="vip-star" aria-label={$t('vip.star')} />
        {/if}
        <!-- sender, subject and snippet are the message's words, not the
             interface's, so each reads in its own direction (#356). The row
             around them keeps the interface direction, so the layout stays put
             while an Arabic subject reads right to left inside it. -->
        <span dir="auto" class="sender">{displayName(message.fromName, message.fromAddress)}</span>
        {#if template === 'single'}
          <span dir="auto" class="subject inline">{message.subject || $t('messageList.noSubject')}</span>
        {/if}
        <span class="meta">
          {#if message.flagColor > 0}
            <span class="color-dot" style={`background:${colorHex}`} aria-hidden="true"></span>
          {/if}
          {#if snoozed}
            <IconClock size={12} stroke={1.7} class="snooze" />
          {/if}
          {#if showOffline}
            <IconDownload size={12} stroke={1.7} class="offline" />
          {/if}
          {#if showFlagIcon}
            <IconFlagFilled size={12} class="flag" />
          {/if}
          {#if message.hasAttachments}
            <IconPaperclip size={12} stroke={1.6} />
          {/if}
          {#if $prefs.showDateTime}
            <time class="time">{formatListDate(message.date, $prefs.timeFormat as TimeFormat)}</time>
          {/if}
        </span>
      </div>

      {#if template !== 'single'}
        <div dir="auto" class="subject">{message.subject || $t('messageList.noSubject')}</div>
      {/if}
      {#if showSnippet}
        <div dir="auto" class="snippet" style={`--preview-lines:${previewLines}`}>{message.snippet}</div>
      {/if}
    </div>
  </div>
</div>

<style>
  .swipe-wrap {
    position: relative;
    overflow: hidden;
  }

  /* the action revealed behind the row while swiping. */
  .reveal {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: 0 var(--space-4);
    color: #fff;
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    background: var(--reveal-color);
  }
  .reveal.right-edge {
    justify-content: flex-end;
  }

  .row {
    position: relative;
    display: flex;
    gap: var(--space-3);
    padding: var(--row-pad-y) var(--row-pad-x);
    border-bottom: var(--hairline) solid var(--border-subtle);
    cursor: var(--cursor-action);
    background: var(--surface-raised);
  }

  /* settle back smoothly after a gesture ends; live dragging has no transition. */
  .row.settling {
    transition: transform 0.18s cubic-bezier(0.22, 1, 0.36, 1);
  }

  /* tighter vertical rhythm as the templates get denser. relaxed keeps the roomy
     default; comfortable is a step tighter (but still shows the snippet, unlike
     compact) so the two read differently. */
  .row.comfortable {
    padding-top: var(--space-2);
    padding-bottom: var(--space-2);
  }
  .row.compact,
  .row.single {
    padding-top: var(--space-2);
    padding-bottom: var(--space-2);
  }
  .row.comfortable .content {
    gap: 1px;
  }

  .row.single {
    align-items: center;
  }

  .row:hover {
    background: var(--surface-hover);
  }

  .row.selected {
    background: var(--selection-bg);
  }

  .row.checked {
    background: var(--selection-bg);
  }

  /* the left edge bar: warning color for a flag, or the label color when set. */
  .row.bar-left::before {
    content: '';
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    width: 3px;
    background: var(--bar-color);
  }

  /* the account's colour on the far edge, clear of the flagged bar. */
  .row.account-mark::after {
    content: '';
    position: absolute;
    top: 0;
    bottom: 0;
    right: 0;
    width: 3px;
    background: var(--account-color);
  }

  .content {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--row-gap);
  }

  /* center alignment keeps the sender and the meta (time, flag, icons) on a stable
     baseline: with baseline alignment, adding the flag icon nudged the date up. */
  .line.top {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .dot {
    width: 7px;
    height: 7px;
    border-radius: 999px;
    background: var(--text-primary);
    flex-shrink: 0;
    align-self: center;
  }

  .sender {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-list);
    color: var(--text-secondary);
    flex: 0 1 auto;
  }

  .row:not(.single) .sender {
    flex: 1;
  }

  .unread .sender {
    color: var(--text-primary);
    font-weight: var(--fw-semibold);
  }

  .meta {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    flex-shrink: 0;
    margin-inline-start: auto;
    color: var(--text-tertiary);
  }

  .meta :global(.flag) {
    color: var(--warning);
  }

  .top :global(.vip-star) {
    flex-shrink: 0;
    margin-inline-end: 2px;
    color: var(--warning);
  }

  .meta :global(.offline) {
    color: var(--success, var(--accent));
  }

  .meta :global(.snooze) {
    color: var(--accent);
  }

  .color-dot {
    width: 8px;
    height: 8px;
    border-radius: 999px;
    flex-shrink: 0;
  }

  .time {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .subject {
    font-size: var(--fz-list);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .subject.inline {
    flex: 1;
    min-width: 0;
    color: var(--text-secondary);
  }

  .unread .subject {
    font-weight: var(--fw-medium);
  }

  .snippet {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    overflow: hidden;
    display: -webkit-box;
    -webkit-line-clamp: var(--preview-lines, 1);
    line-clamp: var(--preview-lines, 1);
    -webkit-box-orient: vertical;
  }
</style>
