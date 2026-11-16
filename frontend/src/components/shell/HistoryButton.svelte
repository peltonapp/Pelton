<script lang="ts">
  // the top bar's undo history: a button beside the activity button that opens
  // the recent reversible actions, newest first. each row can be undone on its
  // own, or redone once undone; a row that cannot be, because a later action
  // touched the same messages, says why instead of hiding the button.
  import historyIcon from '@phosphor-icons/core/assets/duotone/clock-counter-clockwise-duotone.svg?raw'
  import archiveIcon from '@phosphor-icons/core/assets/duotone/archive-duotone.svg?raw'
  import folderIcon from '@phosphor-icons/core/assets/duotone/folder-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import spamIcon from '@phosphor-icons/core/assets/duotone/warning-octagon-duotone.svg?raw'
  import doneIcon from '@phosphor-icons/core/assets/duotone/check-circle-duotone.svg?raw'
  import readIcon from '@phosphor-icons/core/assets/duotone/envelope-open-duotone.svg?raw'
  import flagIcon from '@phosphor-icons/core/assets/duotone/flag-duotone.svg?raw'
  import colorIcon from '@phosphor-icons/core/assets/duotone/palette-duotone.svg?raw'
  import pinIcon from '@phosphor-icons/core/assets/duotone/push-pin-duotone.svg?raw'
  import muteIcon from '@phosphor-icons/core/assets/duotone/bell-slash-duotone.svg?raw'
  import snoozeIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import sendIcon from '@phosphor-icons/core/assets/duotone/paper-plane-tilt-duotone.svg?raw'
  import undoIcon from '@phosphor-icons/core/assets/duotone/arrow-u-up-left-duotone.svg?raw'
  import redoIcon from '@phosphor-icons/core/assets/duotone/arrow-u-up-right-duotone.svg?raw'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import { onDestroy } from 'svelte'
  import Ph from '../common/Ph.svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import { history, historyOpen, redoEntry, undoEntry, type HistoryKind, type HistoryReason } from '../../stores/undohistory'
  import { formatRelative } from '../../lib/format'
  import { bindings } from '../../stores/shortcuts'
  import { tip } from '../../lib/tip'
  import { t, shortcutLabel } from '../../lib/i18n'

  let button: HTMLButtonElement

  const glyphs: Record<HistoryKind, string> = {
    archive: archiveIcon,
    move: folderIcon,
    delete: trashIcon,
    spam: spamIcon,
    done: doneIcon,
    seen: readIcon,
    flag: flagIcon,
    color: colorIcon,
    pin: pinIcon,
    mute: muteIcon,
    snooze: snoozeIcon,
    send: sendIcon,
  }

  // a slow tick keeps the relative times honest while the list is open.
  let now = Date.now()
  const timer = setInterval(() => (now = Date.now()), 30000)
  onDestroy(() => clearInterval(timer))

  $: combo = ($bindings as Record<string, string>)['undo-history'] ?? ''
  $: title = combo ? `${$t('history.open')} (${shortcutLabel(combo)})` : $t('history.open')

  function since(at: number, _now: number): string {
    return formatRelative(at, $t)
  }

  function why(reason: HistoryReason | null): string {
    return reason ? $t(`history.reason.${reason}`) : ''
  }

  function close(): void {
    historyOpen.set(false)
  }
</script>

<button
  type="button"
  class="history"
  class:open={$historyOpen}
  bind:this={button}
  aria-haspopup="dialog"
  aria-expanded={$historyOpen}
  aria-label={$t('history.open')}
  use:tip={{ text: $historyOpen ? '' : title, side: 'bottom' }}
  on:click={() => historyOpen.update((o) => !o)}
>
  <Ph svg={historyIcon} size={15} />
</button>

{#if $historyOpen && button}
  <AnchoredPopover anchor={button} side="below" align="end" label={$t('history.title')} on:close={close}>
    <div class="pop">
      <header>
        <span class="title">{$t('history.title')}</span>
        <span class="tools">
          <button type="button" class="c2-tool compact" aria-label={$t('history.close')} on:click={close}>
            <Ph svg={xIcon} size={13} />
          </button>
        </span>
      </header>

      <div class="body">
        {#if $history.length === 0}
          <p class="empty">{$t('history.empty')}</p>
        {:else}
          <ul aria-label={$t('history.title')}>
            {#each $history as row (row.id)}
              <li class="row" class:undone={row.state === 'undone'} class:dead={row.state === 'failed'}>
                <span class="glyph"><Ph svg={glyphs[row.kind]} size={15} /></span>
                <span class="main">
                  <span class="label">{row.label}</span>
                  <span class="detail">{row.detail}</span>
                  <span class="meta">
                    {#if row.reason && !(row.state === 'undone' && row.reason === 'noRedo')}
                      {why(row.reason)}
                    {:else}
                      {row.state === 'undone' ? `${$t('history.undone')}, ` : ''}{since(row.at, now)}
                    {/if}
                  </span>
                </span>
                {#if row.state === 'undone'}
                  <button
                    type="button"
                    class="c2-tool compact"
                    disabled={!row.canRedo}
                    aria-label={`${$t('history.redo')}: ${row.label}`}
                    use:tip={{ text: row.canRedo ? $t('history.redo') : why(row.reason) }}
                    on:click={() => void redoEntry(row.id)}
                  >
                    <Ph svg={redoIcon} size={14} />
                  </button>
                {:else}
                  <button
                    type="button"
                    class="c2-tool compact"
                    disabled={!row.canUndo}
                    aria-label={`${$t('history.undo')}: ${row.label}`}
                    use:tip={{ text: row.canUndo ? $t('history.undo') : why(row.reason) }}
                    on:click={() => void undoEntry(row.id)}
                  >
                    <Ph svg={undoIcon} size={14} />
                  </button>
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
      </div>
    </div>
  </AnchoredPopover>
{/if}

<style>
  .history {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    height: calc(var(--control-height) - var(--space-2));
    min-width: calc(var(--control-height) - var(--space-2));
    padding: 0 var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    --wails-draggable: no-drag;
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .history:hover,
  .history.open {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .history:active {
    transform: scale(var(--press-scale));
  }

  .pop {
    display: flex;
    flex-direction: column;
    width: var(--popover-width);
    max-width: 100%;
    min-height: 0;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-2) var(--space-2) var(--space-2) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .title {
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
  }

  .tools {
    display: inline-flex;
  }

  .body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-2) 0;
  }

  .empty {
    margin: 0;
    padding: var(--space-5);
    text-align: center;
    color: var(--text-tertiary);
    font-size: var(--fz-label);
  }

  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .row {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-2) var(--space-2) var(--space-4);
  }

  .row:hover {
    background: var(--surface-hover);
  }

  .glyph {
    display: inline-flex;
    padding-top: var(--space-1);
    color: var(--text-tertiary);
  }

  .main {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .label {
    font-size: var(--fz-label);
  }

  .detail {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .meta {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .row.undone .label,
  .row.undone .detail {
    color: var(--text-tertiary);
    text-decoration: line-through;
  }

  .row.dead .label,
  .row.dead .detail {
    color: var(--text-tertiary);
  }
</style>
