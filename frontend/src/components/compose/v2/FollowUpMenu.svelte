<script lang="ts">
  // "remind me if nobody replies", one of the composer's send options. off,
  // it is a quiet toolbar button; set, it names the day the reminder comes
  // and the menu offers to clear it. the presets land at nine in the morning,
  // and a date of your own can be picked under them.
  import { createEventDispatcher } from 'svelte'
  import bellIcon from '@phosphor-icons/core/assets/duotone/bell-ringing-duotone.svg?raw'
  import sunIcon from '@phosphor-icons/core/assets/duotone/sun-duotone.svg?raw'
  import daysIcon from '@phosphor-icons/core/assets/duotone/calendar-dots-duotone.svg?raw'
  import weekIcon from '@phosphor-icons/core/assets/duotone/calendar-blank-duotone.svg?raw'
  import offIcon from '@phosphor-icons/core/assets/duotone/bell-slash-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import AnchoredPopover from '../../common/AnchoredPopover.svelte'
  import DateTimePicker from '../../common/DateTimePicker.svelte'
  import { buildFollowUpPresets, formatSendWhen, type FollowUpPreset } from '../../../lib/composeactions'
  import { shortcutTitle } from '../../../stores/shortcuts'
  import { tip } from '../../../lib/tip'
  import { t } from '../../../lib/i18n'
  import { toastError } from '../../../stores/toast'

  /** The reminder time as an ISO string, or '' for none. */
  export let value = ''
  /** Unique id base for the custom date field. */
  export let id: string
  /** Shows only the icon, for the docked footer. */
  export let compact = false
  /** Opens the menu above the button rather than below. */
  export let menuAbove = false

  const dispatch = createEventDispatcher<{ change: string }>()

  const icons: Record<FollowUpPreset['key'], string> = {
    tomorrow: sunIcon,
    threeDays: daysIcon,
    week: weekIcon,
  }

  let button: HTMLButtonElement
  let menuOpen = false
  let custom = ''

  $: presets = menuOpen ? buildFollowUpPresets(new Date(), $t) : []
  $: set = value !== ''
  $: label = set ? $t('followUp.setFor').replace('{when}', formatSendWhen(new Date(value))) : $t('followUp.button')

  /** Opens the menu, as the follow-up shortcut does. */
  export function open(): void {
    menuOpen = true
    custom = ''
  }

  function pick(when: Date): void {
    if (when.getTime() <= Date.now()) {
      toastError($t('compose.sendLater.pickFutureTime'))
      return
    }
    menuOpen = false
    dispatch('change', when.toISOString())
  }

  function clear(): void {
    menuOpen = false
    dispatch('change', '')
  }
</script>

<button
  type="button"
  class="c2-tool"
  class:compact
  class:set
  bind:this={button}
  aria-label={label}
  aria-haspopup="menu"
  aria-expanded={menuOpen}
  use:tip={{ text: $shortcutTitle($t('followUp.menu'), 'compose-follow-up'), side: menuAbove ? 'top' : 'bottom' }}
  on:click={() => (menuOpen ? (menuOpen = false) : open())}
>
  <Ph svg={bellIcon} size={16} />
  {#if !compact}
    <span>{label}</span>
  {/if}
</button>

{#if menuOpen}
  <AnchoredPopover anchor={button} side={menuAbove ? 'above' : 'below'} align="start" role="menu" label={$t('followUp.menu')} on:close={() => (menuOpen = false)}>
    <div class="menu">
      <p class="lead">{$t('followUp.menu')}</p>
      {#each presets as preset (preset.key)}
        <button type="button" class="preset" role="menuitem" on:click={() => pick(preset.when)}>
          <Ph svg={icons[preset.key]} size={16} />
          <span class="name">{preset.label}</span>
          <span class="when">{formatSendWhen(preset.when)}</span>
        </button>
      {/each}
      {#if set}
        <button type="button" class="preset" role="menuitem" on:click={clear}>
          <Ph svg={offIcon} size={16} />
          <span class="name">{$t('followUp.off')}</span>
        </button>
      {/if}
      <div class="custom">
        <label for={`${id}-follow-up`}>{$t('followUp.pick')}</label>
        <DateTimePicker
          id={`${id}-follow-up`}
          mode="datetime"
          bind:value={custom}
          confirmLabel={$t('followUp.set')}
          on:confirm={() => custom && pick(new Date(custom))}
        />
      </div>
    </div>
  </AnchoredPopover>
{/if}

<style>
  .set {
    color: var(--text-primary);
    background: var(--surface-hover);
  }

  .menu {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    width: calc(var(--popover-width) - var(--space-6));
    padding: var(--space-2);
  }

  .lead {
    margin: 0;
    padding: var(--space-2) var(--space-3) var(--space-1);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .preset {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .preset :global(.ph) {
    color: var(--text-secondary);
  }

  .preset:hover,
  .preset:focus-visible {
    background: var(--surface-hover);
    outline: none;
  }

  .name {
    flex: 1;
  }

  .when {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .custom {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin-top: var(--space-1);
    padding: var(--space-3) var(--space-3) var(--space-2);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .custom label {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }
</style>
