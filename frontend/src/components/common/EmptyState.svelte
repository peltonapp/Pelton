<script lang="ts" context="module">
  /**
   * What an empty state stands for: nothing here yet, a filter or search that
   * matched nothing, a load that failed, or a feature that is not set up.
   */
  export type EmptyKind = 'empty' | 'filtered' | 'error' | 'setup'
</script>

<script lang="ts">
  // the one placeholder every space shows where a list, pane, folder, tab or
  // search has nothing to show. an icon, a title, one sentence saying what
  // belongs here, and up to two actions; the primary one carries its live
  // shortcut. a custom illustration can replace the icon through the default
  // slot.
  import Ph from './Ph.svelte'
  import { shortcutLabel } from '../../lib/i18n'
  import type { ShortcutAction } from '../../lib/shortcuts'
  import { bindings } from '../../stores/shortcuts'
  import { prefs } from '../../stores/prefs'
  import '../../theme/controls.css'

  /** The headline, short and specific to the place. */
  export let title: string
  /** One sentence on what belongs here or what went wrong. */
  export let detail: string = ''
  /** Which case this is; an error is announced assertively. */
  export let kind: EmptyKind = 'empty'
  /** Raw Phosphor svg for the icon; the default slot overrides it. */
  export let icon: string = ''
  /** Full fills and centres in its pane; compact sits inline in a section. */
  export let size: 'full' | 'compact' = 'full'
  /** Heading level of the title. Defaults to 3 when full; compact uses a paragraph. */
  export let level: number | null = null
  /** Label of the primary action; no action is shown without it. */
  export let actionLabel: string = ''
  /** Runs the primary action. */
  export let onAction: (() => void) | null = null
  /** Shortcut that does the same as the primary action, shown from the live bindings. */
  export let shortcut: ShortcutAction | null = null
  /** Label of the secondary action. */
  export let secondaryLabel: string = ''
  /** Runs the secondary action. */
  export let onSecondary: (() => void) | null = null

  $: headingLevel = level ?? (size === 'full' ? 3 : null)
  $: combo = shortcut && $prefs.showShortcutHints ? ($bindings[shortcut] ?? '') : ''
  $: hint = combo ? shortcutLabel(combo) : ''
  $: hasIcon = !!icon || !!$$slots.default
</script>

<div class="empty {size}" class:error={kind === 'error'} role={kind === 'error' ? 'alert' : 'status'} data-kind={kind}>
  {#if hasIcon}
    <div class="icon" aria-hidden="true">
      <slot>
        <Ph svg={icon} size={size === 'full' ? 32 : 18} />
      </slot>
    </div>
  {/if}
  {#if headingLevel}
    <svelte:element this={`h${headingLevel}`} class="title">{title}</svelte:element>
  {:else}
    <p class="title">{title}</p>
  {/if}
  {#if detail}
    <p class="detail">{detail}</p>
  {/if}
  {#if (actionLabel && onAction) || (secondaryLabel && onSecondary)}
    <div class="actions">
      {#if secondaryLabel && onSecondary}
        <button type="button" class="cal-btn" on:click={onSecondary}>{secondaryLabel}</button>
      {/if}
      {#if actionLabel && onAction}
        <button type="button" class="cal-btn" class:primary={kind === 'empty' || kind === 'setup'} on:click={onAction}>
          {actionLabel}
          {#if hint}
            <kbd class="kbd">{hint}</kbd>
          {/if}
        </button>
      {/if}
    </div>
  {/if}
  <slot name="action" />
</div>

<style>
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    height: 100%;
    padding: var(--space-6);
    text-align: center;
    color: var(--text-tertiary);
  }

  .empty.compact {
    align-items: flex-start;
    justify-content: flex-start;
    gap: var(--space-2);
    height: auto;
    padding: var(--space-3) var(--space-2);
    text-align: start;
  }

  .icon {
    display: inline-flex;
    color: var(--text-tertiary);
    opacity: 0.7;
  }

  .title {
    margin: 0;
    font-size: var(--fz-body);
    font-weight: var(--fw-medium);
    color: var(--text-secondary);
  }

  .compact .title {
    font-size: var(--fz-label);
    font-weight: var(--fw-regular);
    color: var(--text-tertiary);
  }

  .error .title {
    color: var(--danger);
  }

  .detail {
    margin: 0;
    max-width: 36ch;
    font-size: var(--fz-label);
    line-height: 1.5;
    word-break: break-word;
  }

  .compact .detail {
    font-size: var(--fz-meta);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: var(--space-2);
    margin-top: var(--space-1);
  }

  .compact .actions {
    justify-content: flex-start;
  }

  .kbd {
    font: inherit;
    font-size: var(--fz-meta);
    opacity: 0.7;
  }
</style>
