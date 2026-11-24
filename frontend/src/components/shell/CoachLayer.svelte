<script lang="ts">
  // the shortcut coach of the new design. Holding the platform modifier alone
  // for a moment brings up the sheet of shortcuts for what is on screen, gone
  // again on release or any other key; its own shortcut pins the sheet open
  // with a filter. After an action done by mouse, a small hint near the bottom
  // names its key. Nothing here ever takes a key press from the app.
  import { onDestroy, onMount } from 'svelte'
  import { fly } from 'svelte/transition'
  import CoachSheet from './CoachSheet.svelte'
  import { coachHint, sheetMode } from '../../stores/coach'
  import { bindings, recording } from '../../stores/shortcuts'
  import { prefs } from '../../stores/prefs'
  import { space, spaceActionEntries } from '../../stores/space'
  import { visibleMessageId, tabs } from '../../stores/tabs'
  import { selectedIds } from '../../stores/listselect'
  import { orderedSpaces, visibleSpaces } from '../../lib/spaces'
  import { shortcuts as registry } from '../../lib/shortcuts'
  import { buildSheet, filterSheet, type SheetContext, type SheetGroup, type SheetSource } from '../../lib/coach'
  import { dur, motion } from '../../lib/motion'
  import { isMac, shortcutLabel, t } from '../../lib/i18n'

  /** Keeps the sheet from opening while a full-window screen is up. */
  export let suspended: boolean = false

  /** How long the modifier has to be held alone. */
  const holdMs = 1000
  const modifier = isMac ? 'Meta' : 'Control'

  let holdTimer: ReturnType<typeof setTimeout> | undefined
  let ctx: SheetContext | null = null
  let query = ''

  function labelFor(action: string): string {
    return registry.find((s) => s.action === action)?.labelKey ?? action
  }

  const arrows: Record<string, string> = { arrowup: '↑', arrowdown: '↓', arrowleft: '←', arrowright: '→' }

  function format(combo: string): string {
    return arrows[combo] ?? shortcutLabel(combo)
  }

  // context reads what is on screen at the moment the sheet opens.
  function context(): SheetContext {
    const active = document.activeElement
    return {
      space: $space,
      spaces: $orderedSpaces.map((s) => s.key),
      numbered: $visibleSpaces.map((s) => s.key),
      message: $visibleMessageId !== null || $selectedIds.size > 0,
      composer: document.querySelector('[data-compose-id]') !== null,
      composerFocused: !!active?.closest?.('[data-compose-id]'),
      vim: $prefs.appVimMode,
      tabs: $tabs.length > 0,
      splits: $prefs.splitInbox,
      spaceActions: spaceActionEntries($space).map((e) => ({ action: e.action, labelKey: e.labelKey ?? labelFor(e.action) })),
    }
  }

  $: if ($sheetMode === 'closed') {
    ctx = null
    query = ''
  } else if (ctx === null) {
    ctx = context()
  }

  $: source = { bindings: $bindings as Record<string, string>, labelFor, format } satisfies SheetSource

  // a filter searches everything there is a key for, not only what is on
  // screen, so a shortcut is never out of reach of the search.
  function everything(c: SheetContext, src: SheetSource): SheetGroup[] {
    const all = buildSheet({ ...c, space: 'mail', message: true, composer: true, composerFocused: false }, src)
    const own = c.space === 'mail' ? [] : buildSheet(c, src).filter((g) => g.id === 'space')
    return [...all, ...own]
  }

  $: groups = ctx ? (query.trim() ? filterSheet(everything(ctx, source), query, $t) : buildSheet(ctx, source)) : []

  function cancelHold(): void {
    clearTimeout(holdTimer)
    holdTimer = undefined
    if ($sheetMode === 'hold') {
      sheetMode.set('closed')
    }
  }

  function bareModifier(event: KeyboardEvent): boolean {
    const other = isMac ? event.ctrlKey : event.metaKey
    return event.key === modifier && !event.shiftKey && !event.altKey && !other
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === modifier && event.repeat) {
      return
    }
    if (bareModifier(event)) {
      clearTimeout(holdTimer)
      if ($prefs.coachHold && $sheetMode === 'closed' && !$recording && !suspended) {
        holdTimer = setTimeout(() => {
          holdTimer = undefined
          sheetMode.set('hold')
        }, holdMs)
      }
      return
    }
    cancelHold()
  }

  function onKeyup(event: KeyboardEvent): void {
    if (event.key === modifier) {
      cancelHold()
    }
  }

  onMount(() => {
    window.addEventListener('keydown', onKeydown, true)
    window.addEventListener('keyup', onKeyup, true)
    window.addEventListener('pointerdown', cancelHold, true)
    window.addEventListener('blur', cancelHold)
  })

  onDestroy(() => {
    window.removeEventListener('keydown', onKeydown, true)
    window.removeEventListener('keyup', onKeyup, true)
    window.removeEventListener('pointerdown', cancelHold, true)
    window.removeEventListener('blur', cancelHold)
    clearTimeout(holdTimer)
  })

  // the hint's text is a template so the key can sit anywhere in it.
  $: hintParts = $t('coach.hint').split('{key}')
  $: hintAction = $coachHint ? $t($coachHint.labelKey) : ''
  // with toasts at the bottom centre, the hint moves out of their way.
  $: hintTop = $prefs.toastPosition === 'bottom-center'
</script>

{#if $sheetMode !== 'closed'}
  <CoachSheet
    {groups}
    pinned={$sheetMode === 'pinned'}
    pinKey={$bindings['shortcut-sheet'] ? format($bindings['shortcut-sheet']) : ''}
    bind:query
    on:close={() => sheetMode.set('closed')}
  />
{/if}

{#if $coachHint}
  {#key $coachHint.id}
    <div
      class="hint"
      class:top={hintTop}
      role="status"
      transition:fly={{ y: hintTop ? -6 : 6, duration: dur(motion.base) }}
    >
      <span>{(hintParts[0] ?? '').replace('{action}', hintAction)}</span>
      <kbd>{format($coachHint.combo)}</kbd>
      {#if hintParts[1]}
        <span>{hintParts[1].replace('{action}', hintAction)}</span>
      {/if}
    </div>
  {/key}
{/if}

<style>
  .hint {
    position: fixed;
    left: 50%;
    bottom: var(--coach-hint-offset);
    transform: translateX(-50%);
    z-index: 850;
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    color: var(--text-secondary);
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    white-space: nowrap;
    pointer-events: none;
  }

  .hint.top {
    bottom: auto;
    top: calc(var(--topbar-height) + var(--space-4));
  }

  kbd {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: var(--chip-height);
    height: var(--chip-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-strong);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-primary);
  }
</style>
