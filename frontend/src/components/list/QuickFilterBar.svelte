<script lang="ts">
  // the quick filter bar: toggles and a text box that narrow the messages
  // already loaded in the list, without running a search.
  import envelopeIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
  import flagIcon from '@phosphor-icons/core/assets/duotone/flag-duotone.svg?raw'
  import clipIcon from '@phosphor-icons/core/assets/duotone/paperclip-duotone.svg?raw'
  import bookIcon from '@phosphor-icons/core/assets/duotone/address-book-duotone.svg?raw'
  import funnelIcon from '@phosphor-icons/core/assets/duotone/funnel-simple-duotone.svg?raw'
  import xIcon from '@phosphor-icons/core/assets/duotone/x-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { t } from '../../lib/i18n'
  import { quickFilterActive, type QuickFilter } from '../../lib/quickfilter'
  import { loadContactAddresses, quickFilter, resetQuickFilter } from '../../stores/quickfilter'

  type Toggle = 'unread' | 'flagged' | 'attachments' | 'contacts'

  const toggles: { key: Toggle; icon: string; label: string }[] = [
    { key: 'unread', icon: envelopeIcon, label: 'messageList.search.chip.isUnread' },
    { key: 'flagged', icon: flagIcon, label: 'sidebar.view.flagged' },
    { key: 'attachments', icon: clipIcon, label: 'quickFilter.attachments' },
    { key: 'contacts', icon: bookIcon, label: 'quickFilter.contacts' },
  ]

  function flip(key: Toggle): void {
    const next: QuickFilter = { ...$quickFilter, [key]: !$quickFilter[key] }
    quickFilter.set(next)
    if (key === 'contacts' && next.contacts) {
      void loadContactAddresses()
    }
  }

  function onText(event: Event): void {
    quickFilter.update((f) => ({ ...f, text: (event.currentTarget as HTMLInputElement).value }))
  }

  function onKey(event: KeyboardEvent): void {
    if (event.key === 'Escape' && $quickFilter.text !== '') {
      event.stopPropagation()
      quickFilter.update((f) => ({ ...f, text: '' }))
    }
  }
</script>

<div class="bar" role="toolbar" aria-label={$t('quickFilter.label')}>
  {#each toggles as toggle (toggle.key)}
    <button
      type="button"
      class="toggle"
      class:on={$quickFilter[toggle.key]}
      aria-pressed={$quickFilter[toggle.key]}
      on:click={() => flip(toggle.key)}
    >
      <Ph svg={toggle.icon} size={13} />
      <span>{$t(toggle.label)}</span>
    </button>
  {/each}
  <label class="text">
    <Ph svg={funnelIcon} size={13} />
    <input
      type="text"
      autocomplete="off"
      spellcheck="false"
      aria-label={$t('quickFilter.text')}
      placeholder={$t('quickFilter.text')}
      value={$quickFilter.text}
      on:input={onText}
      on:keydown={onKey}
    />
  </label>
  {#if quickFilterActive($quickFilter)}
    <button type="button" class="reset" aria-label={$t('quickFilter.clear')} title={$t('quickFilter.clear')} on:click={resetQuickFilter}>
      <Ph svg={xIcon} size={13} />
    </button>
  {/if}
</div>

<style>
  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1);
    padding-block: var(--space-2);
    padding-inline: var(--row-pad-x) var(--space-2);
    border-bottom: var(--hairline) solid var(--border-subtle);
    background: var(--surface-raised);
    animation: open var(--motion-base) var(--ease-out);
  }

  @keyframes open {
    from {
      opacity: 0;
      transform: translateY(calc(var(--space-2) * -1));
    }
  }

  .toggle {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    height: calc(var(--chip-height) + var(--space-1));
    padding: 0 var(--space-2);
    border: var(--hairline) solid transparent;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    white-space: nowrap;
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .toggle:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .toggle:active {
    transform: scale(0.96);
  }

  .toggle.on {
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .text {
    flex: 1;
    min-width: calc(var(--space-6) * 4);
    display: flex;
    align-items: center;
    gap: var(--space-2);
    height: calc(var(--chip-height) + var(--space-1));
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-tertiary);
    transition: border-color var(--motion-fast) var(--ease-out);
  }

  .text:focus-within {
    border-color: var(--accent);
  }

  input {
    flex: 1;
    min-width: 0;
    padding: 0;
    border: none;
    outline: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-meta);
  }

  .reset {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-1);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
  }

  .reset:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
</style>
