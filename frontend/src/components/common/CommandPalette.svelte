<script lang="ts">
  // The command palette (#134): one keyboard-driven surface for every action,
  // mailbox, view and settings pane. It opens as a bare input near the top of
  // the window and only fills in once something is typed.
  //
  // Entries whose target has to be chosen (rename which mailbox, which colour)
  // return a step instead of acting, and the palette descends into it rather
  // than closing, with the parent entry shown as a chip in front of the input.
  import { tick } from 'svelte'
  import { fade, fly } from 'svelte/transition'
  import { IconChevronRight } from '@tabler/icons-svelte'
  import {
    paletteOpen,
    paletteQuery,
    paletteStep,
    closePalette,
    rankCommands,
    rankStep,
    recordUse,
    parseQuery,
    requestMailSearch,
    paletteMailBusy,
    paletteQuickSelect,
    quickSelectMax,
    type RankedCommand,
  } from '../../stores/palette'
  import type { PaletteCommand, CommandGroup } from '../../lib/commands'
  import { highlightRuns } from '../../lib/fuzzy'
  import { t, isMac, shortcutLabel } from '../../lib/i18n'

  /** Every command currently available, rebuilt by the parent as state changes. */
  export let commands: PaletteCommand[] = []
  /** Mail hits for the current "/" query, already in relevance order. */
  export let mail: PaletteCommand[] = []

  let input: HTMLInputElement | null = null
  let listEl: HTMLDivElement | null = null
  let active = 0

  $: parsed = parseQuery($paletteQuery)
  // mail is asked for explicitly with "/" and comes back already ranked, so it
  // bypasses the fuzzy matcher entirely.
  $: if (!$paletteStep && parsed.group === 'mail') {
    requestMailSearch(parsed.text)
  }
  $: results = $paletteStep
    ? rankStep($paletteStep, $paletteQuery)
    : parsed.group === 'mail'
      ? mail.map((command) => ({ command, score: 0, positions: [] }))
      : rankCommands(commands, $paletteQuery)

  // a changed result set invalidates the highlight position, so it goes back to
  // the top rather than pointing at whatever happens to be at the old index.
  $: $paletteQuery, $paletteStep, (active = 0)
  $: if (active >= ordered.length) {
    active = Math.max(0, ordered.length - 1)
  }

  $: if ($paletteOpen) {
    void focusInput()
  }

  // group headers only earn their space when more than one group is showing.
  $: groups = groupRuns(results)
  // the flattened display order. keyboard navigation walks this rather than
  // `results`, whose pure score order is not the order on screen.
  $: ordered = groups.flatMap((g) => g.items)
  $: prefix = parsed.group

  async function focusInput(): Promise<void> {
    await tick()
    input?.focus()
  }

  // the order the groups appear in, whatever their scores. Doing something
  // comes before going somewhere, which comes before changing a preference:
  // typing three letters should not put a settings pane above the action of
  // the same name.
  const groupRank: CommandGroup[] = ['action', 'mail', 'navigate', 'setting']

  /**
   * Buckets ranked results by group. The buckets are in a fixed order rather
   * than by their strongest member, so the palette reads the same way every
   * time; within a bucket the score still decides.
   */
  function groupRuns(list: RankedCommand[]): { group: CommandGroup; items: RankedCommand[] }[] {
    const buckets = new Map<CommandGroup, RankedCommand[]>()
    for (const entry of list) {
      const bucket = buckets.get(entry.command.group)
      if (bucket) {
        bucket.push(entry)
      } else {
        buckets.set(entry.command.group, [entry])
      }
    }
    return [...buckets.entries()]
      .map(([group, items]) => ({ group, items }))
      .sort((a, b) => groupRank.indexOf(a.group) - groupRank.indexOf(b.group))
  }

  function groupLabel(group: CommandGroup): string {
    if (group === 'action') {
      return $t('palette.group.actions')
    }
    if (group === 'navigate') {
      return $t('palette.group.navigate')
    }
    if (group === 'mail') {
      return $t('palette.group.mail')
    }
    return $t('palette.group.settings')
  }

  /**
   * The quick-select key a row advertises: the first runs on Enter, the next
   * nine on mod+1 through mod+9. Rows past that have none.
   */
  function quickKey(index: number): string {
    if (!$paletteQuickSelect) {
      return ''
    }
    if (index === 0) {
      return isMac ? '\u21a9' : 'Enter'
    }
    return index <= quickSelectMax ? shortcutLabel(`mod+${index}`) : ''
  }

  /** A result's position in the display order, so one row highlights at a time. */
  function indexOf(entry: RankedCommand): number {
    return ordered.indexOf(entry)
  }

  // one command at a time: a second Enter while the first is still running
  // would run it again.
  let running = false
  async function run(command: PaletteCommand): Promise<void> {
    if (running) {
      return
    }
    running = true
    try {
      recordUse(command.id)
      const next = await command.run()
      if (next) {
        paletteStep.set(next)
        paletteQuery.set('')
        await focusInput()
        return
      }
      closePalette()
    } finally {
      running = false
    }
  }

  /** Leaves the current step, or closes the palette when at the top level. */
  function back(): void {
    if ($paletteStep) {
      paletteStep.set(null)
      paletteQuery.set('')
      void focusInput()
    } else {
      closePalette()
    }
  }

  async function move(delta: number): Promise<void> {
    if (ordered.length === 0) {
      return
    }
    active = (active + delta + ordered.length) % ordered.length
    await tick()
    listEl?.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest' })
  }

  function onKeydown(event: KeyboardEvent): void {
    switch (event.key) {
      case 'Escape':
        event.preventDefault()
        back()
        break
      case 'ArrowDown':
        event.preventDefault()
        void move(1)
        break
      case 'ArrowUp':
        event.preventDefault()
        void move(-1)
        break
      case 'Enter': {
        // the Enter that confirms an input method's composition (Chinese,
        // Japanese, Korean) is not a choice
        if (event.isComposing || event.keyCode === 229) {
          break
        }
        const chosen = ordered[active]
        if (chosen) {
          event.preventDefault()
          void run(chosen.command)
        }
        break
      }
      default: {
        // mod+1..9 jumps straight to a row, Alfred style. stopPropagation keeps
        // the combo from also reaching a user binding on the same keys.
        if (!$paletteQuickSelect) {
          break
        }
        const mod = isMac ? event.metaKey : event.ctrlKey
        if (!mod || event.altKey || event.shiftKey) {
          break
        }
        const digit = Number(event.key)
        if (!Number.isInteger(digit) || digit < 1 || digit > quickSelectMax) {
          break
        }
        const target = ordered[digit]
        if (target) {
          event.preventDefault()
          event.stopPropagation()
          void run(target.command)
        }
        break
      }
      case 'Backspace':
        // backspacing past the start of an empty step query leaves the step,
        // which is the only way out other than Escape.
        if ($paletteStep && $paletteQuery === '') {
          event.preventDefault()
          back()
        }
        break
    }
  }
</script>

{#if $paletteOpen}
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
  <div class="backdrop" transition:fade={{ duration: 100 }} on:click={closePalette}></div>
  <div
    class="palette"
    role="dialog"
    aria-modal="true"
    aria-label={$t('palette.title')}
    transition:fly={{ y: -8, duration: 140 }}
  >
    <div class="field">
      {#if $paletteStep}
        <span class="chip">
          {$paletteStep.label}
          <IconChevronRight size={13} stroke={2} />
        </span>
      {:else if prefix}
        <span class="chip">{groupLabel(prefix)}</span>
      {/if}
      <!-- svelte-ignore a11y-autofocus -->
      <input
        bind:this={input}
        bind:value={$paletteQuery}
        type="text"
        autofocus
        spellcheck="false"
        autocomplete="off"
        role="combobox"
        aria-expanded={results.length > 0}
        aria-controls="palette-results"
        aria-activedescendant={ordered[active] ? `palette-row-${active}` : undefined}
        placeholder={$paletteStep ? $paletteStep.placeholder : $t('palette.placeholder')}
        on:keydown={onKeydown}
      />
    </div>

    {#if results.length > 0}
      <div class="results" id="palette-results" role="listbox" aria-label={$t('palette.title')} bind:this={listEl}>
        {#each groups as run_ (run_.group + run_.items[0].command.id)}
          {#if groups.length > 1}
            <div class="group">{groupLabel(run_.group)}</div>
          {/if}
          {#each run_.items as entry (entry.command.id)}
            {@const i = indexOf(entry)}
            <!-- svelte-ignore a11y-click-events-have-key-events -->
            <div
              class="row"
              class:danger={entry.command.danger}
              id="palette-row-{i}"
              role="option"
              aria-selected={i === active}
              data-active={i === active}
              tabindex="-1"
              on:click={() => void run(entry.command)}
              on:mousemove={() => (active = i)}
            >
              <span class="icon">
                {#if entry.command.swatch}
                  <span class="swatch" style="background: {entry.command.swatch}"></span>
                {:else}
                  <svelte:component this={entry.command.icon} size={16} stroke={1.6} />
                {/if}
              </span>
              <span class="label">
                {#each highlightRuns(entry.command.label, entry.positions) as part}
                  {#if part.hit}<mark>{part.text}</mark>{:else}{part.text}{/if}
                {/each}
              </span>
              {#if entry.command.hint}
                <span class="hint">{entry.command.hint}</span>
              {/if}
              {#if quickKey(i)}
                <kbd class="quick">{quickKey(i)}</kbd>
              {/if}
            </div>
          {/each}
        {/each}
      </div>
    {:else if prefix === 'mail' && $paletteMailBusy}
      <p class="empty">{$t('palette.searching')}</p>
    {:else if parsed.text !== ''}
      <p class="empty">{$t('palette.noResults')}</p>
    {/if}
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 400;
    background: var(--scrim, rgba(0, 0, 0, 0.4));
  }

  .palette {
    position: fixed;
    z-index: 401;
    top: 12vh;
    left: 50%;
    transform: translateX(-50%);
    width: min(620px, calc(100vw - 2 * var(--space-5)));
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    overflow: hidden;
  }

  .field {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    flex-shrink: 0;
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .field input {
    flex: 1;
    min-width: 0;
    border: none;
    background: transparent;
    font-family: var(--font-ui);
    font-size: var(--fz-title);
    color: var(--text-primary);
  }
  .field input:focus {
    outline: none;
  }
  .field input::placeholder {
    color: var(--text-tertiary);
  }

  .results {
    max-height: 50vh;
    overflow-y: auto;
    border-top: var(--hairline) solid var(--border-default);
    padding: var(--space-2);
  }

  .group {
    padding: var(--space-2) var(--space-2) var(--space-1);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    color: var(--text-tertiary);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-2);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
    color: var(--text-primary);
  }
  .row[data-active='true'] {
    background: var(--surface-hover);
  }
  .row.danger {
    color: var(--danger);
  }

  .icon {
    display: inline-flex;
    flex-shrink: 0;
    color: var(--text-secondary);
  }
  .row.danger .icon {
    color: var(--danger);
  }

  .swatch {
    width: 14px;
    height: 14px;
    border-radius: 50%;
  }

  .label {
    flex: 1;
    min-width: 0;
    font-size: var(--fz-body);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .label mark {
    background: transparent;
    color: var(--accent);
    font-weight: var(--fw-semibold);
  }

  .hint {
    flex-shrink: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .quick {
    flex-shrink: 0;
    min-width: 2.2em;
    padding: 1px var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-family: var(--font-ui);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    text-align: center;
  }

  .empty {
    margin: 0;
    padding: var(--space-4);
    border-top: var(--hairline) solid var(--border-default);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    text-align: center;
  }
</style>
