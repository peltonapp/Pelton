<script lang="ts">
  // one row in the message list. its layout follows the chosen row template
  // (relaxed, comfortable, compact, single) with per-field overrides for the
  // avatar and snippet and a clamped preview-line count. unread shows by weight
  // and a dot (never accent). flagged rows highlight per the user's chosen style
  // (flag icon, a left bar, both icon+bar, or off); a color label paints the left
  // bar and a small dot. offline and snoozed states show little meta icons.
  // trackpad swipes (horizontal wheel) reveal a configurable action, and in
  // the new design quick buttons lie over the date while the row is hovered or
  // has the keyboard focus.
  import { createEventDispatcher, onDestroy } from 'svelte'
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
  import type { MessageSummary, RowTemplate, SwipeAction, ThreadSummary } from '../../lib/types'
  import { t, shortcutLabel } from '../../lib/i18n'
  import { vipSenders, bareAddress } from '../../stores/vip'
  import Highlight from '../common/Highlight.svelte'
  import Ph from '../common/Ph.svelte'
  import caretIcon from '@phosphor-icons/core/assets/duotone/caret-right-duotone.svg?raw'
  import repliedIcon from '@phosphor-icons/core/assets/duotone/arrow-bend-up-left-duotone.svg?raw'
  import forwardedIcon from '@phosphor-icons/core/assets/duotone/arrow-bend-up-right-duotone.svg?raw'
  import sentIcon from '@phosphor-icons/core/assets/duotone/paper-plane-tilt-duotone.svg?raw'
  import taskIcon from '@phosphor-icons/core/assets/duotone/check-square-duotone.svg?raw'
  import noteIcon from '@phosphor-icons/core/assets/duotone/note-duotone.svg?raw'
  import mutedIcon from '@phosphor-icons/core/assets/duotone/bell-slash-duotone.svg?raw'
  import pinIcon from '@phosphor-icons/core/assets/duotone/push-pin-duotone.svg?raw'
  import { taskMailMarks } from '../../stores/tasks'
  import { projectMailMarks, projects } from '../../stores/projects'
  import { projectColor } from '../../lib/projectlogic'
  import { highlightTerms } from '../../stores/search'
  import { idleSwipe, swipeWheel, swipeOutcome, swiping, anySwipe, swipeTuning, type SwipeState } from '../../lib/swipe'
  import { isRowAction, rowActionApplies, rowActionView, type RowActionId } from '../../lib/rowactions'
  import { spamState } from '../../lib/messageactions'
  import { currentUIScale } from '../../theme/theme'
  import { dur, motion } from '../../lib/motion'
  import { bindings } from '../../stores/shortcuts'
  import { tip } from '../../lib/tip'
  import type { ShortcutAction } from '../../lib/shortcuts'

  export let message: MessageSummary
  export let selected: boolean = false
  /** The list draws a sliding band over this row, so the row skips its own selected fill. */
  export let banded: boolean = false
  // checked reflects membership in the bulk-selection set (modifier-click).
  export let checked: boolean = false
  // accountColor marks which mailbox the message is from in a list that mixes
  // them (#486); '' shows nothing.
  export let accountColor: string = ''
  /** The conversation this row stands for in a grouped list, null for a lone message row. */
  export let thread: ThreadSummary | null = null
  /** The conversation's other messages are shown beneath this row. */
  export let expanded: boolean = false
  /** The row is one of an expanded conversation's messages, drawn indented. */
  export let child: boolean = false
  /** The message was sent by the user, shown as theirs inside a conversation. */
  export let fromMe: boolean = false
  /** The row has just been revealed by expanding its conversation. */
  export let entering: boolean = false
  /** The quick buttons to offer over the row, in order; empty offers none. */
  export let actions: RowActionId[] = []
  /** The row has the list's keyboard focus, so its quick buttons take Tab. */
  export let active: boolean = false

  const dispatch = createEventDispatcher<{
    swipe: 'left' | 'right'
    rowaction: { id: RowActionId; via: 'button' | 'swipe' }
    toggle: void
  }>()

  $: newDesign = $prefs.design !== 'classic'
  $: threadCount = thread?.count ?? 0
  $: expandable = threadCount > 1
  // the conversation holds unread mail this row's own message does not show.
  $: threadUnread = (thread?.unreadCount ?? 0) > 0 && message.seen
  $: answered = newDesign && (thread ? thread.answered : message.answered)
  $: forwarded = newDesign && (thread ? thread.forwarded : message.forwarded)
  // whether a task links to this mail: undefined for none, true once every
  // such task is done.
  $: taskMark = newDesign ? $taskMailMarks.get(message.id) : undefined
  $: pinned = newDesign && !child && (!!message.pinned || !!thread?.pinned)
  // the project the conversation is filed under, the first when several.
  $: projectIds = newDesign && !child && message.threadId ? ($projectMailMarks.get(message.threadId) ?? []) : []
  $: inProject = projectIds.length ? $projects.find((p) => p.id === projectIds[0]) : undefined

  // a reply under its conversation repeats the subject above it, so it shows
  // what it says instead.
  $: secondLine = child && !showSnippet && message.snippet ? message.snippet : message.subject || $t('messageList.noSubject')
  $: countLabel =
    thread && thread.unreadCount > 0
      ? $t('thread.countUnread').replace('{count}', String(threadCount)).replace('{unread}', String(thread.unreadCount))
      : $t('thread.count').replace('{count}', String(threadCount))

  function onToggle(event: MouseEvent): void {
    event.stopPropagation()
    dispatch('toggle')
  }

  $: unread = !message.seen
  $: template = $prefs.rowTemplate as RowTemplate

  // the classic design only knows a picture column that is on or off; the new
  // one can also set a small picture into the sender line.
  $: avatarPlacement =
    $prefs.design === 'classic' ? ($prefs.rowShowAvatar ? 'column' : 'hidden') : $prefs.avatarPlacement
  $: showAvatar = avatarPlacement === 'column' && template !== 'single'
  $: inlineAvatar = avatarPlacement === 'inline'
  $: showSnippet =
    $prefs.rowShowSnippet &&
    (template === 'relaxed' || template === 'comfortable') &&
    (!!message.snippet || $prefs.design !== 'classic')
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

  // trackpad swipes. lib/swipe.ts reads the wheel events: it decides the axis,
  // follows a horizontal swipe, ignores the momentum tail and says what the
  // swipe comes to. a trackpad has no release event, so the gesture ends once
  // the events stop for a moment. the new design has a short and a long swipe
  // each way; the classic one keeps a single action per direction past a
  // deliberate commit line.
  let wrapEl: HTMLDivElement
  let wrapWidth = 320
  let gesture: SwipeState = idleSwipe
  let offset = 0
  let settling = false
  let committed = false
  let rtl = false
  let releaseTimer: ReturnType<typeof setTimeout> | undefined

  $: swipeActions = newDesign
    ? {
        leftShort: $prefs.swipeLeftShort,
        leftLong: $prefs.swipeLeftLong,
        rightShort: $prefs.swipeRightShort,
        rightLong: $prefs.swipeRightLong,
      }
    : { leftShort: $prefs.swipeLeftAction, leftLong: 'none', rightShort: $prefs.swipeRightAction, rightLong: 'none' }
  $: tuning = newDesign ? swipeTuning : { ...swipeTuning, shortFraction: 0.6 }
  $: swipeOn = $prefs.swipeEnabled && anySwipe(swipeActions)
  $: outcome = swipeOutcome(offset, wrapWidth, swipeActions, rtl, tuning)
  // the action the uncovered side stands for, named before the first mark is
  // reached too: the first one further along that does something.
  $: previewAction = outcome.action !== 'none' ? outcome.action : nextAction(offset)

  function nextAction(at: number): string {
    const sign = at < 0 ? -1 : 1
    for (const reach of [tuning.shortFraction, tuning.longFraction]) {
      const ahead = swipeOutcome(sign * wrapWidth * reach, wrapWidth, swipeActions, rtl, tuning)
      if (ahead.action !== 'none') {
        return ahead.action
      }
    }
    return 'none'
  }

  $: classicMeta = {
    none: { label: '', color: 'var(--surface-hover)', icon: IconFlag },
    delete: { label: $t('action.delete'), color: 'var(--danger)', icon: IconTrash },
    read: { label: $t('messageList.swipe.read'), color: 'var(--accent)', icon: IconMailOpened },
    unread: { label: $t('messageList.swipe.unread'), color: 'var(--accent)', icon: IconMailFilled },
    flag: { label: $t('messageList.flag'), color: 'var(--warning)', icon: IconFlagFilled },
    archive: { label: $t('action.archive'), color: 'var(--text-tertiary)', icon: IconArchive },
    snooze: { label: $t('shortcut.snooze'), color: 'var(--accent)', icon: IconClockPause },
  } as Record<SwipeAction, { label: string; color: string; icon: typeof IconTrash }>
  $: classicReveal = classicMeta[(offset > 0 ? $prefs.swipeRightAction : $prefs.swipeLeftAction) as SwipeAction] ?? classicMeta.none

  $: rowState = {
    seen: message.seen,
    flagged: message.flagged,
    pinned: !!message.pinned || !!thread?.pinned,
    muted: !!message.muted,
    inSpam: newDesign && spamState(message) === 'spam',
  }
  $: swipeView = isRowAction(previewAction) ? rowActionView(previewAction, rowState) : null

  // the actions that take the row out of the list slide it the rest of the
  // way; the others let it settle back once they ran.
  const leaving = new Set(['archive', 'delete', 'done', 'spam'])

  function onWheel(event: WheelEvent): void {
    if (!swipeOn || committed || event.deltaMode !== 0 || event.ctrlKey) {
      return
    }
    if (gesture.phase === 'idle' || gesture.phase === 'vertical') {
      rtl = getComputedStyle(wrapEl).direction === 'rtl'
    }
    const scale = currentUIScale()
    const step = swipeWheel(gesture, { dx: event.deltaX / scale, dy: event.deltaY / scale, t: event.timeStamp }, wrapWidth, tuning)
    gesture = step.state
    if (!step.consume) {
      return
    }
    event.preventDefault()
    settling = false
    offset = gesture.offset
    clearTimeout(releaseTimer)
    releaseTimer = setTimeout(release, tuning.gapMs)
  }

  function release(): void {
    if (!swiping(gesture) || committed) {
      return
    }
    const result = swipeOutcome(offset, wrapWidth, swipeActions, rtl, tuning)
    gesture = idleSwipe
    settling = true
    if (result.action === 'none') {
      offset = 0
      return
    }
    committed = true
    const still = dur(motion.base) === 0
    const out = newDesign ? leaving.has(result.action) : result.action === 'delete' || result.action === 'archive'
    offset = out && !still ? Math.sign(offset) * wrapWidth : offset
    setTimeout(
      () => {
        if (newDesign && isRowAction(result.action)) {
          dispatch('rowaction', { id: result.action, via: 'swipe' })
        } else {
          dispatch('swipe', result.dir)
        }
        offset = 0
        committed = false
      },
      still ? 0 : motion.fast,
    )
  }

  onDestroy(() => clearTimeout(releaseTimer))

  // quick buttons over the row. the list hands over which to show, already
  // cut to what applies; they are only reachable by Tab on the focused row.
  $: shownActions = actions.filter((id) => rowActionApplies(id, { threadId: message.threadId, spam: id === 'spam' ? spamState(message) : 'none' }))
  $: actionViews = shownActions.map((id) => rowActionView(id, rowState))
  $: rowLabel = `${displayName(message.fromName, message.fromAddress)}, ${message.subject || $t('messageList.noSubject')}`

  function keysFor(action: ShortcutAction, map: Record<string, string>): string | undefined {
    const combo = map[action]
    return combo ? shortcutLabel(combo) : undefined
  }

  function runAction(event: MouseEvent, id: RowActionId): void {
    event.stopPropagation()
    dispatch('rowaction', { id, via: 'button' })
  }

  // Enter and Space press the focused button; the list must not also open or
  // select the row with them.
  function onActionKey(event: KeyboardEvent): void {
    if (event.key === 'Enter' || event.key === ' ') {
      event.stopPropagation()
    }
  }
</script>

<!-- keyboard navigation for the list is handled by the listbox container
     (arrow keys + enter), so the row itself only needs the click affordance. -->
<div class="swipe-wrap" class:swiping={offset !== 0} class:active bind:this={wrapEl} bind:clientWidth={wrapWidth}>
  {#if offset !== 0 && newDesign}
    <div
      class="reveal new"
      class:right-edge={offset < 0}
      class:armed={outcome.stage !== 'none'}
      class:danger={swipeView?.danger}
      class:long={outcome.stage === 'long'}
      aria-hidden="true"
    >
      {#if swipeView}
        {#key outcome.stage}
          <span class="reveal-mark"><Ph svg={swipeView.icon} size={18} /></span>
        {/key}
        <span>{$t(swipeView.labelKey)}</span>
      {/if}
    </div>
  {:else if offset !== 0}
    <div
      class="reveal"
      class:right-edge={offset < 0}
      style={`--reveal-color:${classicReveal.color}`}
      aria-hidden="true"
    >
      <svelte:component this={classicReveal.icon} size={18} stroke={1.7} />
      <span>{classicReveal.label}</span>
    </div>
  {/if}
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-noninteractive-element-interactions -->
  <div
    class="row {template}"
    class:selected
    class:banded
    class:unread
    class:checked
    class:bar-left={showBar}
    class:account-mark={accountColor !== ''}
    class:settling
    class:child
    class:entering
    class:threaded={thread !== null}
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

    {#if child}
      <span class="guide" aria-hidden="true"></span>
    {/if}
    <div class="content">
      <div class="line top">
        {#if expandable && !child}
          <button
            type="button"
            class="twisty"
            class:open={expanded}
            tabindex="-1"
            aria-expanded={expanded}
            aria-label={expanded ? $t('thread.collapse') : $t('thread.expand')}
            title={expanded ? $t('thread.collapse') : $t('thread.expand')}
            on:click={onToggle}
            on:dblclick|stopPropagation
          >
            <Ph svg={caretIcon} size={12} />
          </button>
        {/if}
        {#if unread}
          <span class="dot" aria-hidden="true"></span>
        {:else if threadUnread}
          <span class="dot ring" aria-hidden="true"></span>
        {/if}
        {#if fromMe}
          <span class="sent" title={$t('thread.sent')}><Ph svg={sentIcon} size={12} /></span>
        {/if}
        {#if isVip}
          <IconStarFilled size={12} class="vip-star" aria-label={$t('vip.star')} />
        {/if}
        <!-- sender, subject and snippet are the message's words, not the
             interface's, so each reads in its own direction (#356). The row
             around them keeps the interface direction, so the layout stays put
             while an Arabic subject reads right to left inside it. -->
        {#if inlineAvatar}
          <Avatar name={message.fromName} email={message.fromAddress} size={16} />
        {/if}
        <span dir="auto" class="sender"><Highlight text={fromMe ? $t('thread.me') : displayName(message.fromName, message.fromAddress)} terms={$highlightTerms} /></span>
        {#if template === 'single'}
          <span dir="auto" class="subject inline"><Highlight text={secondLine} terms={$highlightTerms} /></span>
        {/if}
        <span class="meta">
          {#if inProject}
            <span
              class="project"
              style={`--project:${projectColor(inProject)}`}
              title={projectIds.length > 1 ? $t('projects.chip.many').replace('{name}', inProject.name).replace('{n}', String(projectIds.length - 1)) : $t('projects.chip.one').replace('{name}', inProject.name)}
            >
              <span class="project-dot" aria-hidden="true"></span><span class="project-name">{inProject.name}</span>
            </span>
          {/if}
          {#if expandable && !child && thread}
            <span
              class="count"
              class:has-unread={thread.unreadCount > 0}
              title={countLabel}
              aria-label={countLabel}
            >{threadCount}</span>
          {/if}
          {#if answered}
            <span class="marker" title={$t('thread.replied')} aria-label={$t('thread.replied')}><Ph svg={repliedIcon} size={12} /></span>
          {/if}
          {#if forwarded}
            <span class="marker" title={$t('thread.forwarded')} aria-label={$t('thread.forwarded')}><Ph svg={forwardedIcon} size={12} /></span>
          {/if}
          {#if taskMark !== undefined}
            <span class="marker task" class:done={taskMark} title={$t(taskMark ? 'tasks.mailMarkDone' : 'tasks.mailMark')} aria-label={$t(taskMark ? 'tasks.mailMarkDone' : 'tasks.mailMark')}>
              <Ph svg={taskIcon} size={12} />
            </span>
          {/if}
          {#if newDesign && message.hasNote}
            <span class="marker" title={$t('note.mark')} aria-label={$t('note.mark')}><Ph svg={noteIcon} size={12} /></span>
          {/if}
          {#if newDesign && message.muted}
            <span class="marker" title={$t('mute.mark')} aria-label={$t('mute.mark')}><Ph svg={mutedIcon} size={12} /></span>
          {/if}
          {#if pinned}
            <span class="marker" title={$t('pin.mark')} aria-label={$t('pin.mark')}><Ph svg={pinIcon} size={12} /></span>
          {/if}
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
        <div dir="auto" class="subject" class:reply-text={child}><Highlight text={secondLine} terms={$highlightTerms} /></div>
      {/if}
      {#if showSnippet}
        {#if message.snippet}
          <div dir="auto" class="snippet" style={`--preview-lines:${previewLines}`}><Highlight text={message.snippet} terms={$highlightTerms} /></div>
        {:else}
          <div class="snippet none" style={`--preview-lines:${previewLines}`}>{$t('messageList.noText')}</div>
        {/if}
      {/if}
    </div>
  </div>
  {#if actionViews.length > 0 && offset === 0}
    <!-- a sibling of the option rather than inside it, so the listbox keeps
         plain options and the buttons are their own toolbar. -->
    <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
    <div
      class="row-actions {template}"
      role="toolbar"
      tabindex="-1"
      aria-label={$t('rowActions.label').replace('{row}', rowLabel)}
      on:pointerdown|stopPropagation
      on:dblclick|stopPropagation
      on:keydown={onActionKey}
    >
      {#each actionViews as view (view.id)}
        <button
          type="button"
          class="row-action"
          class:danger={view.danger}
          tabindex={active ? 0 : -1}
          aria-label={$t(view.labelKey)}
          use:tip={{ text: $t(view.labelKey), shortcut: keysFor(view.shortcut, $bindings), side: 'top' }}
          on:click={(e) => runAction(e, view.id)}
        >
          <Ph svg={view.icon} size={15} />
        </button>
      {/each}
    </div>
  {/if}
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

  /* the new design's reveal stays quiet until the swipe passes its first
     mark and fills once it has, so the point where letting go acts is plain
     to see. a long swipe changes the action and lands the mark again. */
  .reveal.new {
    background: var(--surface-sunken);
    color: var(--text-tertiary);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .reveal.new.armed {
    background: var(--accent);
    color: var(--accent-fg);
  }

  .reveal.new.armed.danger {
    background: var(--danger);
    color: var(--text-inverse);
  }

  .reveal-mark {
    display: inline-flex;
  }

  .reveal.new.armed .reveal-mark {
    animation: mark-in var(--motion-base) var(--ease-spring);
  }

  @keyframes mark-in {
    from {
      transform: scale(0.6);
    }
  }

  :global(:root[data-design='new']) .row.settling {
    transition: transform var(--motion-slow) var(--ease-spring);
  }

  /* the quick buttons lie over the end of the top line, the date included,
     on the row's own colour with a short fade into the text before them, so
     nothing in the row moves when they appear. the list's hover and
     selection bands tint them along with the row. */
  .row-actions {
    position: absolute;
    top: calc(var(--row-pad-y) - var(--space-1));
    inset-inline-end: var(--row-pad-x);
    display: flex;
    align-items: center;
    gap: var(--space-1);
    padding-inline-start: var(--space-1);
    background: var(--surface-raised);
    opacity: 0;
    pointer-events: none;
    transition: opacity var(--motion-fast) var(--ease-out);
  }

  .row-actions::before {
    content: '';
    position: absolute;
    top: 0;
    bottom: 0;
    inset-inline-end: 100%;
    width: var(--row-action-fade);
    background: var(--surface-raised);
    -webkit-mask-image: linear-gradient(to left, currentColor, transparent);
    mask-image: linear-gradient(to left, currentColor, transparent);
    pointer-events: none;
  }

  :global([dir='rtl']) .row-actions::before {
    -webkit-mask-image: linear-gradient(to right, currentColor, transparent);
    mask-image: linear-gradient(to right, currentColor, transparent);
  }

  .row-actions.comfortable,
  .row-actions.compact {
    top: calc(var(--space-2) - var(--space-1));
  }

  .row-actions.single {
    top: 50%;
    transform: translateY(-50%);
  }

  .swipe-wrap:hover .row-actions,
  .row-actions:focus-within,
  :global([role='listbox']:focus-visible) .swipe-wrap.active .row-actions {
    opacity: 1;
    pointer-events: auto;
  }

  :global(body.mail-dragging) .row-actions {
    opacity: 0;
    pointer-events: none;
  }

  .row-action {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--row-action-size);
    height: var(--row-action-size);
    padding: 0;
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .row-action:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .row-action.danger:hover {
    background: var(--danger-bg);
    color: var(--danger);
  }

  .row-action:active {
    transform: scale(var(--press-scale));
  }

  .row-action:focus-visible {
    outline: calc(var(--hairline) * 2) solid var(--accent);
    outline-offset: calc(var(--hairline) * -1);
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

  .row.banded:not(.checked) {
    background: var(--surface-raised);
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

  /* conversations. only a row that can be expanded has the control; the others
     start at the row's edge like their subject line does, and replies indent
     by the control's width. */
  .twisty {
    flex-shrink: 0;
    width: var(--thread-twisty);
    height: var(--thread-twisty);
    margin-inline-start: calc(var(--space-2) * -1);
  }

  .twisty {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0;
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .twisty:hover {
    background: var(--surface-sunken);
    color: var(--text-primary);
  }

  .twisty :global(.ph) {
    transition: transform var(--motion-base) var(--ease-out);
  }

  .twisty.open :global(.ph) {
    transform: rotate(90deg);
  }

  :global([dir='rtl']) .twisty:not(.open) :global(.ph) {
    transform: scaleX(-1);
  }

  .row.child .content {
    padding-inline-start: calc(var(--thread-twisty) + var(--space-2));
  }

  /* a hairline down the reply column ties the replies to their conversation. */
  .guide {
    position: absolute;
    top: 0;
    bottom: 0;
    inset-inline-start: calc(var(--row-pad-x) + var(--thread-twisty) / 2 - var(--space-2));
    border-inline-start: var(--hairline) solid var(--border-strong);
    pointer-events: none;
  }

  .row.child .sender {
    font-size: var(--fz-label);
  }

  .row.entering {
    animation: reply-in var(--motion-base) var(--ease-out) both;
  }

  @keyframes reply-in {
    from {
      opacity: 0;
      transform: translateY(calc(var(--space-2) * -1));
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .row.entering,
    .twisty :global(.ph) {
      animation: none;
      transition: none;
    }
  }

  .dot.ring {
    background: transparent;
    box-shadow: inset 0 0 0 calc(var(--hairline) * 3) var(--text-secondary);
  }

  .sent {
    display: inline-flex;
    flex-shrink: 0;
    color: var(--text-tertiary);
  }

  .count {
    flex-shrink: 0;
    min-width: var(--chip-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
    line-height: 1.5;
    text-align: center;
    font-variant-numeric: tabular-nums;
    color: var(--text-tertiary);
  }

  .count.has-unread {
    color: var(--text-primary);
    font-weight: var(--fw-semibold);
    border-color: var(--border-strong);
  }

  .marker {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  .marker.task {
    color: var(--accent);
  }

  .marker.task.done {
    color: var(--text-tertiary);
  }

  /* the project a conversation is filed under: a dot in its color and its
     name, cut short so the date keeps its place. */
  .project {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    max-width: calc(var(--space-6) * 4);
    padding: 0 var(--space-1);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    line-height: 1.5;
  }

  .project-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .project-dot {
    flex-shrink: 0;
    width: 0.55em;
    height: 0.55em;
    border-radius: 50%;
    background: var(--project);
  }

  .subject.reply-text {
    color: var(--text-secondary);
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

  /* the new design keeps every row the same height, so a message with a short
     or missing preview still takes the lines the others do. */
  :global(:root[data-design='new']) .snippet {
    line-height: 1.4;
    min-height: calc(var(--preview-lines, 1) * 1.4em);
  }

  .snippet.none {
    font-style: italic;
  }

  /* hover is a band the list slides over the rows. */
  :global(:root[data-design='new']) .row:hover:not(.selected):not(.checked) {
    background: var(--surface-raised);
  }
</style>
