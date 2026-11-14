<script lang="ts">
  // three sample rows of the message list that follow every list choice: the
  // row template, sender picture, preview lines, flag marking and the date.
  import flagIcon from '@phosphor-icons/core/assets/duotone/flag-duotone.svg?raw'
  import Avatar from '../../common/Avatar.svelte'
  import Ph from '../../common/Ph.svelte'
  import { prefs } from '../../../stores/prefs'
  import { formatListDate, type TimeFormat } from '../../../lib/format'
  import type { RowTemplate } from '../../../lib/types'

  const samples = [
    {
      name: 'Ada Lovelace',
      email: 'ada@analytical.example',
      subject: 'Notes on the Analytical Engine',
      snippet:
        'I have been thinking about how the machine might weave algebraic patterns, just as the loom weaves flowers and leaves. The notes run longer than the translation they accompany, and I would value your reading of the final section before they go to print.',
      minutes: 4,
      unread: true,
      flagged: true,
    },
    {
      name: 'Grace Hopper',
      email: 'grace@compiler.example',
      subject: 'Compiler release schedule',
      snippet:
        'The first release candidate is built and the regression suite is green on every target. Remaining work is the documentation pass and the migration notes for anyone still on the old calling convention, which I would like to finish this week.',
      minutes: 95,
      unread: false,
      flagged: false,
    },
    {
      name: 'Alan Turing',
      email: 'alan@bletchley.example',
      subject: 'Lunch on Friday?',
      snippet:
        'Are we still on for that place near the park? They have the good fries, and the table by the window is usually free until one. Let me know and I will book it.',
      minutes: 60 * 26,
      unread: false,
      flagged: true,
    },
  ]

  $: template = $prefs.rowTemplate as RowTemplate
  $: placement = $prefs.avatarPlacement
  $: column = placement === 'column' && template !== 'single'
  $: inline = placement === 'inline'
  $: snippet = $prefs.rowShowSnippet && (template === 'relaxed' || template === 'comfortable')
  $: flag = $prefs.flagHighlight
  $: columnSize = template === 'relaxed' ? 36 : 30

  function when(minutes: number): string {
    return formatListDate(new Date(Date.now() - minutes * 60000).toISOString(), $prefs.timeFormat as TimeFormat)
  }
</script>

<div class="list" aria-hidden="true">
  {#each samples as s, i (s.email)}
    <div
      class="row {template}"
      class:unread={s.unread}
      class:selected={i === 0}
      class:bar={s.flagged && (flag === 'left' || flag === 'both')}
    >
      {#if column}
        <Avatar name={s.name} email={s.email} size={columnSize} />
      {/if}
      <div class="content">
        <div class="top">
          {#if s.unread}
            <span class="dot"></span>
          {/if}
          {#if inline}
            <Avatar name={s.name} email={s.email} size={16} />
          {/if}
          <span class="from">{s.name}</span>
          {#if template === 'single'}
            <span class="subject inline">{s.subject}</span>
          {/if}
          <span class="meta">
            {#if s.flagged && (flag === 'flag' || flag === 'both')}
              <span class="flag"><Ph svg={flagIcon} size={12} /></span>
            {/if}
            {#if $prefs.showDateTime}
              <span class="time">{when(s.minutes)}</span>
            {/if}
          </span>
        </div>
        {#if template !== 'single'}
          <div class="subject">{s.subject}</div>
        {/if}
        {#if snippet}
          <div class="snippet" style={`--lines:${$prefs.previewLines}`}>{s.snippet}</div>
        {/if}
      </div>
    </div>
  {/each}
</div>

<style>
  .list {
    margin-bottom: var(--space-6);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    overflow: hidden;
  }

  .row {
    position: relative;
    display: flex;
    gap: var(--space-3);
    padding: var(--row-pad-y) var(--row-pad-x);
    border-bottom: var(--hairline) solid var(--border-subtle);
    transition: padding var(--motion-base) var(--ease-out);
  }

  .row:last-child {
    border-bottom: none;
  }

  .row.selected {
    background: var(--selection-bg);
  }

  .row.comfortable {
    padding-top: var(--space-2);
    padding-bottom: var(--space-2);
  }

  .row.compact,
  .row.single {
    padding-top: var(--space-1);
    padding-bottom: var(--space-1);
  }

  .row.bar::before {
    content: '';
    position: absolute;
    top: 0;
    bottom: 0;
    inset-inline-start: 0;
    width: 3px;
    background: var(--warning);
  }

  .content {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .row.single .content {
    justify-content: center;
  }

  .top {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accent);
    flex-shrink: 0;
  }

  .from {
    font-size: var(--fz-list);
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .unread .from {
    color: var(--text-primary);
    font-weight: var(--fw-semibold);
  }

  .meta {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    margin-inline-start: auto;
    flex-shrink: 0;
    color: var(--text-tertiary);
  }

  .flag {
    display: inline-flex;
    color: var(--warning);
  }

  .time {
    font-size: var(--fz-meta);
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
    -webkit-line-clamp: var(--lines, 1);
    line-clamp: var(--lines, 1);
    -webkit-box-orient: vertical;
  }
</style>
