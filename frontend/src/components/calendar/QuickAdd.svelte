<script lang="ts">
  // quick add: a command bar for writing an event as a line of text, such as
  // "dentist fri 9am". what would be created is drawn underneath as an event
  // card that follows the typing, with the pieces that were understood listed
  // as chips. a pasted event link or JSCalendar text shows the events it
  // carries instead. Enter creates, Shift+Enter opens the full editor, escape
  // closes. the date parser loads the first time this opens; nothing typed
  // leaves the app.
  import { createEventDispatcher, onDestroy, onMount } from 'svelte'
  import { fade, fly } from 'svelte/transition'
  import lightningIcon from '@phosphor-icons/core/assets/duotone/lightning-duotone.svg?raw'
  import clockIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import repeatIcon from '@phosphor-icons/core/assets/duotone/repeat-duotone.svg?raw'
  import mapPinIcon from '@phosphor-icons/core/assets/duotone/map-pin-duotone.svg?raw'
  import bellIcon from '@phosphor-icons/core/assets/duotone/bell-duotone.svg?raw'
  import infoIcon from '@phosphor-icons/core/assets/duotone/info-duotone.svg?raw'
  import '../../theme/controls.css'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import Select from '../common/Select.svelte'
  import { parsePhrase, type ChronoLike, type PhraseResult, type PartKind } from '../../lib/eventphrase'
  import { loadChrono } from '../../lib/chronoload'
  import { decodeEventLink, decodeJSCalendar } from '../../lib/api'
  import { calendarColor, formatWhen } from '../../lib/calformat'
  import { describeRRule } from '../../lib/recurrence'
  import { parseEventTime } from '../../lib/caldate'
  import { popPopup, pushPopup } from '../../lib/popups'
  import { reminderLabel } from './labels'
  import { localZone, newDraft } from './occurrence'
  import { errorMessage } from '../../stores/toast'
  import { dur, motion } from '../../lib/motion'
  import { locale, t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { Calendar, EventDraft } from '../../lib/types'

  /** The day a line without a date lands on: the day on screen or a clicked one. */
  export let base: Date
  /** A clicked time slot on that day, minutes after midnight. */
  export let baseMinutes: number | null = null
  /** Calendars an event can go in, and the one chosen. */
  export let calendars: Calendar[]
  export let calendarId: number
  export let timeFormat: TimeFormat = 'auto'
  /** The reminder a new event gets when the line names none; negative for none. */
  export let defaultReminder: number = -1
  /** Fixed for tests; the clock otherwise. */
  export let now: () => Date = () => new Date()

  const dispatch = createEventDispatcher<{ close: void; create: EventDraft[]; edit: EventDraft }>()

  const linkPattern = /^https?:\/\/\S+#z[A-Za-z0-9_-]+$/

  let text = ''
  let input: HTMLInputElement
  let parsers: ChronoLike[] | null = null
  let loadError = ''

  // a pasted link or JSCalendar text: what it decoded to, for the text it was.
  let bundle: { text: string; kind: 'link' | 'jscal'; drafts: EventDraft[]; skipped: number; error: string } | null = null
  let bundleSeq = 0

  onMount(() => {
    pushPopup()
    input?.focus()
    loadChrono($locale)
      .then((list) => (parsers = list))
      .catch((err) => (loadError = errorMessage(err)))
  })

  onDestroy(popPopup)

  $: trimmed = text.trim()
  let special: 'link' | 'jscal' | null = null
  $: special = linkPattern.test(trimmed) ? 'link' : /^[[{]/.test(trimmed) && trimmed.includes('@type') ? 'jscal' : null
  $: if (special) {
    void decode(trimmed, special)
  }
  $: phrase = !special && parsers && trimmed ? parsePhrase(trimmed, { now: now(), base, baseMinutes, lang: $locale, chrono: parsers }) : null
  $: single = phrase ? draftOf(phrase, calendarId) : null
  $: shownBundle = bundle && bundle.text === trimmed ? bundle : null
  $: drafts = special ? (shownBundle?.drafts ?? []).map((d) => ({ ...d, calendarId })) : single ? [single] : []
  $: color = calendarColor(calendars.find((c) => c.id === calendarId))
  $: calendarItems = calendars.map((c) => ({ value: String(c.id), label: c.name || $t('calendar.defaultName'), color: calendarColor(c) }))

  async function decode(value: string, kind: 'link' | 'jscal'): Promise<void> {
    const seq = ++bundleSeq
    try {
      const res = kind === 'link' ? await decodeEventLink(value) : await decodeJSCalendar(value)
      if (seq === bundleSeq) {
        bundle = { text: value, kind, drafts: res.events.map(fromBundle), skipped: res.skipped, error: '' }
      }
    } catch (err) {
      if (seq === bundleSeq) {
        bundle = { text: value, kind, drafts: [], skipped: 0, error: errorMessage(err) }
      }
    }
  }

  function fromBundle(d: EventDraft): EventDraft {
    return {
      ...d,
      id: 0,
      calendarId,
      timeZone: d.allDay ? '' : localZone(),
      categories: d.categories ?? [],
      reminders: d.reminders ?? [],
      attendees: d.attendees ?? [],
      scope: '',
      recurrenceId: '',
      force: false,
    }
  }

  function draftOf(p: PhraseResult, cal: number): EventDraft {
    const reminder = p.reminderMinutes ?? defaultReminder
    return { ...newDraft(cal, p.start, p.end, p.allDay, reminder), summary: p.title, location: p.location, rrule: p.rrule }
  }

  function create(): void {
    if (drafts.length > 0) {
      dispatch('create', drafts)
    }
  }

  function edit(): void {
    if (drafts.length > 0) {
      dispatch('edit', drafts[0])
    }
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      dispatch('close')
    } else if (event.key === 'Enter') {
      event.preventDefault()
      if (event.shiftKey) {
        edit()
      } else {
        create()
      }
    }
  }

  const partKeys: Record<PartKind, string> = {
    date: 'quickAdd.part.date',
    duration: 'quickAdd.part.duration',
    repeat: 'quickAdd.part.repeat',
    reminder: 'quickAdd.part.reminder',
    location: 'quickAdd.part.location',
    allDay: 'calendar.allDay',
  }

  function when(d: EventDraft): string {
    return formatWhen(parseEventTime(d.start, d.allDay), parseEventTime(d.end, d.allDay), d.allDay, timeFormat)
  }

  function repeatText(d: EventDraft): string {
    return d.rrule ? describeRRule(d.rrule, parseEventTime(d.start, d.allDay), $t, navigator.language) : ''
  }

  function reminderText(d: EventDraft): string {
    return d.reminders.map((r) => reminderLabel(r.minutesBefore, $t)).join(', ')
  }

  $: bundleNote = shownBundle
    ? shownBundle.error
      ? $t('quickAdd.badData')
      : $t(shownBundle.kind === 'link' ? 'quickAdd.fromLink' : 'quickAdd.fromJscal').replace('{n}', String(shownBundle.drafts.length)) +
        (shownBundle.skipped ? ` ${$t('quickAdd.skipped').replace('{n}', String(shownBundle.skipped))}` : '')
    : ''
</script>

<!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
<div class="layer" transition:fade={{ duration: dur(motion.fast) }} on:click|self={() => dispatch('close')}>
  <div class="bar" role="dialog" aria-modal="true" aria-label={$t('quickAdd.title')} in:fly={{ y: -8, duration: dur(motion.base) }}>
    <div class="field">
      <span class="lead" aria-hidden="true"><Ph svg={lightningIcon} size={16} /></span>
      <input
        bind:this={input}
        bind:value={text}
        placeholder={$t('quickAdd.placeholder')}
        aria-label={$t('quickAdd.title')}
        aria-describedby="quick-add-preview"
        autocomplete="off"
        spellcheck="false"
        on:keydown={onKeydown}
      />
    </div>

    <div class="body" id="quick-add-preview" aria-live="polite">
      {#if loadError}
        <p class="quiet">{loadError}</p>
      {:else if !trimmed}
        <p class="quiet">{$t('quickAdd.hint')}</p>
      {:else if !special && !parsers}
        <p class="quiet"><PieProgress label={$t('calendar.loading')} /></p>
      {:else}
        {#if special}
          {#if bundleNote}
            <p class="quiet"><Ph svg={infoIcon} size={13} />{bundleNote}</p>
          {/if}
        {:else if phrase}
          {#if phrase.parts.length > 0}
            <ul class="parts" aria-label={$t('quickAdd.recognized')}>
              {#each phrase.parts as part (part.start)}
                <li class="part {part.kind}"><span class="kind">{$t(partKeys[part.kind])}</span>{part.text}</li>
              {/each}
            </ul>
          {/if}
          {#if !phrase.dated}
            <p class="quiet"><Ph svg={infoIcon} size={13} />{$t('quickAdd.notDated').replace('{when}', when(drafts[0]))}</p>
          {/if}
        {/if}
        {#each drafts.slice(0, 6) as d, i (i)}
          <article class="card" style:--cal={color}>
            <h3>{d.summary || $t('calendar.untitled')}</h3>
            <p><Ph svg={clockIcon} size={13} />{when(d)}</p>
            {#if d.rrule}
              <p><Ph svg={repeatIcon} size={13} />{repeatText(d)}</p>
            {/if}
            {#if d.location}
              <p><Ph svg={mapPinIcon} size={13} />{d.location}</p>
            {/if}
            {#if d.reminders.length > 0}
              <p><Ph svg={bellIcon} size={13} />{reminderText(d)}</p>
            {/if}
          </article>
        {/each}
        {#if drafts.length > 6}
          <p class="quiet">{$t('calendar.more').replace('{n}', String(drafts.length - 6))}</p>
        {/if}
      {/if}
    </div>

    <footer>
      {#if calendarItems.length > 1}
        <Select class="ctl" value={String(calendarId)} items={calendarItems} ariaLabel={$t('calendar.calendar')} on:change={(e) => (calendarId = Number(e.detail))} />
      {:else if calendarItems.length === 1}
        <span class="cal-name"><span class="dot" style:--cal={color}></span>{calendarItems[0].label}</span>
      {/if}
      <span class="spacer"></span>
      <button type="button" class="cal-btn" disabled={drafts.length === 0} on:click={edit}>
        {$t('quickAdd.openEditor')}<kbd>⇧↵</kbd>
      </button>
      <button type="button" class="cal-btn primary" disabled={drafts.length === 0} on:click={create}>
        {drafts.length > 1 ? $t('quickAdd.createMany').replace('{n}', String(drafts.length)) : $t('quickAdd.create')}<kbd>↵</kbd>
      </button>
    </footer>
  </div>
</div>

<style>
  .layer {
    position: fixed;
    inset: 0;
    z-index: 300;
    display: flex;
    justify-content: center;
    align-items: flex-start;
    padding: calc(var(--topbar-height) + var(--space-6)) var(--space-4) var(--space-4);
    background: var(--scrim);
  }

  .bar {
    display: flex;
    flex-direction: column;
    width: min(calc(var(--topsearch-width) * 1.25), 100%);
    max-height: 100%;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    overflow: hidden;
  }

  .field {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 0 var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .lead {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  input {
    flex: 1;
    min-width: 0;
    height: calc(var(--control-height) + var(--space-4));
    border: none;
    outline: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
  }

  input::placeholder {
    color: var(--text-tertiary);
  }

  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    min-height: 0;
    padding: var(--space-3) var(--space-4);
    overflow-y: auto;
  }

  .quiet {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .parts {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .part {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--chip-height);
    padding-inline: var(--space-2);
    border-radius: var(--radius-control);
    background: var(--selection-bg);
    font-size: var(--fz-meta);
    color: var(--text-primary);
    white-space: nowrap;
    animation: chip-in var(--motion-base) var(--ease-out);
  }

  .kind {
    color: var(--text-tertiary);
  }

  @keyframes chip-in {
    from {
      opacity: 0;
      transform: scale(0.9);
    }
  }

  .card {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--space-2) var(--space-3);
    border-inline-start: var(--cal-edge) solid var(--cal);
    border-radius: var(--radius-control);
    background: color-mix(in srgb, var(--cal) 10%, var(--surface-raised));
  }

  .card h3 {
    margin: 0;
    font-size: var(--fz-list);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  .card p {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-secondary);
    font-variant-numeric: tabular-nums;
  }

  .card p :global(svg) {
    flex-shrink: 0;
    color: var(--text-tertiary);
  }

  footer {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-4);
    border-top: var(--hairline) solid var(--border-subtle);
    background: var(--surface-raised);
  }

  footer :global(.ctl) {
    height: calc(var(--control-height) - 4px);
    padding: 0 var(--space-2);
    border-color: transparent;
    background: transparent;
    font-size: var(--fz-label);
  }

  footer :global(.ctl:hover),
  footer :global(.ctl.open) {
    border-color: var(--border-default);
  }

  .cal-name {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .dot {
    width: var(--space-3);
    height: var(--space-3);
    border-radius: 50%;
    background: var(--cal);
  }

  .spacer {
    flex: 1;
  }

  kbd {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    opacity: 0.7;
  }
</style>
