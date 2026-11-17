<script lang="ts">
  // the look step of onboarding: theme, accent, density, how the panes are
  // drawn and the interface size. every pick applies to the whole app at once,
  // so the preview beside the step and the step itself both show it.
  import Ph from '../common/Ph.svelte'
  import ColorPicker from '../common/ColorPicker.svelte'
  import ChoiceCards from '../settings2/ui/ChoiceCards.svelte'
  import ShellSkeleton from '../settings2/skeletons/ShellSkeleton.svelte'
  import Segments from './Segments.svelte'
  import monitorIcon from '@phosphor-icons/core/assets/duotone/monitor-duotone.svg?raw'
  import sunIcon from '@phosphor-icons/core/assets/duotone/sun-duotone.svg?raw'
  import moonIcon from '@phosphor-icons/core/assets/duotone/moon-duotone.svg?raw'
  import clockIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import paletteIcon from '@phosphor-icons/core/assets/duotone/palette-duotone.svg?raw'
  import {
    prefs,
    setTheme,
    setThemeDarkTimes,
    setAccent,
    setDensity,
    setPaneStyle,
    setUIScale,
  } from '../../stores/prefs'
  import { accentPresets, accentTone, canonicalAccent, darkScheme, normalizeHex } from '../../theme/accent'
  import { t } from '../../lib/i18n'
  import type { DensityPref, PaneStyle, ThemePref } from '../../lib/types'

  const themeIcons: Record<string, string> = { system: monitorIcon, light: sunIcon, dark: moonIcon, schedule: clockIcon }

  $: themes = [
    { key: 'system', label: $t('onboarding.theme.system') },
    { key: 'light', label: $t('onboarding.theme.light') },
    { key: 'dark', label: $t('onboarding.theme.dark') },
    { key: 'schedule', label: $t('settingsPanel.theme.schedule') },
  ]
  $: densities = [
    { key: 'compact', label: $t('onboarding.density.compact') },
    { key: 'medium', label: $t('onboarding.density.medium') },
    { key: 'luxe', label: $t('onboarding.density.luxe') },
  ]
  $: paneStyles = [
    { key: 'hybrid', label: $t('panes.hybrid'), hint: $t('panes.hybrid.hint') },
    { key: 'flat', label: $t('panes.flat'), hint: $t('panes.flat.hint') },
    { key: 'soft', label: $t('panes.soft'), hint: $t('panes.soft.hint') },
  ]
  $: scales = [
    { key: '0.9', label: $t('settingsPanel.scale.90') },
    { key: '1', label: $t('settingsPanel.scale.100') },
    { key: '1.1', label: $t('settingsPanel.scale.110') },
    { key: '1.17', label: $t('settingsPanel.scale.117') },
    { key: '1.25', label: $t('settingsPanel.scale.125') },
    { key: '1.5', label: $t('settingsPanel.scale.150') },
  ]

  // the spacing token each density card spreads its bars by, so the cards
  // compare honestly.
  const densityGaps: Record<string, string> = { compact: 'var(--space-1)', medium: 'var(--space-2)', luxe: 'var(--space-3)' }

  let pickerOpen = false
  $: presets = accentPresets($prefs.design)
  $: current = canonicalAccent($prefs.accent, $prefs.design)
  $: isPreset = presets.some((c) => normalizeHex(c) === current)

  function onSwatchKey(event: KeyboardEvent, index: number): void {
    const forward = event.key === 'ArrowRight' || event.key === 'ArrowDown'
    const backward = event.key === 'ArrowLeft' || event.key === 'ArrowUp'
    if (!forward && !backward) {
      return
    }
    event.preventDefault()
    const next = presets[(index + (forward ? 1 : -1) + presets.length) % presets.length]
    setAccent(next)
    const group = (event.currentTarget as HTMLElement).parentElement
    group?.querySelector<HTMLElement>(`[data-hex="${next}"]`)?.focus()
  }

  /** Whether the accent popover is open, so the host can let Escape close it first. */
  export function closePopover(): boolean {
    if (pickerOpen) {
      pickerOpen = false
      return true
    }
    return false
  }
</script>

<section class="group">
  <h3 class="label">{$t('settingsPanel.label.theme')}</h3>
  {#if $prefs.themeId}
    <p class="hint">{$t('themes.baseLockedHint')}</p>
  {:else}
    <ChoiceCards
      label={$t('settingsPanel.label.theme')}
      value={$prefs.theme}
      options={themes}
      minWidth={96}
      on:change={(e) => setTheme(e.detail as ThemePref)}
      let:option
    >
      <span class="art icon"><Ph svg={themeIcons[option.key]} size={22} /></span>
    </ChoiceCards>
    {#if $prefs.theme === 'schedule'}
      <div class="times">
        <label class="time">
          <span>{$t('settingsPanel.label.darkFrom')}</span>
          <input
            type="time"
            value={$prefs.themeDarkStart}
            on:change={(e) => setThemeDarkTimes(e.currentTarget.value, $prefs.themeDarkEnd)}
          />
        </label>
        <label class="time">
          <span>{$t('settingsPanel.label.darkUntil')}</span>
          <input
            type="time"
            value={$prefs.themeDarkEnd}
            on:change={(e) => setThemeDarkTimes($prefs.themeDarkStart, e.currentTarget.value)}
          />
        </label>
      </div>
    {/if}
  {/if}
</section>

<section class="group">
  <h3 class="label">{$t('settingsPanel.accentLabel')}</h3>
  <div class="swatches" role="radiogroup" aria-label={$t('settingsPanel.accentPresets')}>
    {#each presets as hex, index (hex)}
      {@const on = normalizeHex(hex) === current}
      <button
        type="button"
        role="radio"
        class="swatch"
        class:on
        style={`--sw:${accentTone(hex, $prefs.design, $darkScheme)}`}
        aria-checked={on}
        aria-label={`${$t('settingsPanel.accentLabel')} ${hex}`}
        tabindex={on || (!isPreset && index === 0) ? 0 : -1}
        data-hex={hex}
        on:click={() => setAccent(hex)}
        on:keydown={(e) => onSwatchKey(e, index)}
      >
        <svg viewBox="0 0 12 12" aria-hidden="true"><path d="M2.5 6.4 5 8.8 9.6 3.4" /></svg>
      </button>
    {/each}
    <button
      type="button"
      class="swatch custom"
      class:on={!isPreset}
      aria-expanded={pickerOpen}
      aria-label={$t('onboarding.customColor')}
      on:click={() => (pickerOpen = !pickerOpen)}
    >
      <Ph svg={paletteIcon} size={16} />
    </button>
  </div>
  {#if pickerOpen}
    <div class="picker">
      <ColorPicker value={$prefs.accent} on:change={(e) => setAccent(e.detail)} />
    </div>
  {/if}
</section>

<section class="group">
  <h3 class="label">{$t('settingsPanel.label.density')}</h3>
  <ChoiceCards
    label={$t('settingsPanel.label.density')}
    value={$prefs.density}
    options={densities}
    minWidth={96}
    on:change={(e) => setDensity(e.detail as DensityPref)}
    let:option
  >
    <span class="art bars" style={`gap:${densityGaps[option.key]}`}>
      <i></i><i class="short"></i><i></i>
    </span>
  </ChoiceCards>
</section>

{#if $prefs.design === 'new'}
  <section class="group">
    <h3 class="label">{$t('panes.label')}</h3>
    <ChoiceCards
      label={$t('panes.label')}
      value={$prefs.paneStyle}
      options={paneStyles}
      minWidth={130}
      on:change={(e) => setPaneStyle(e.detail as PaneStyle)}
      let:option
    >
      <ShellSkeleton
        layout={$prefs.layoutMode}
        nav={$prefs.navStyle}
        railSide={$prefs.railSide}
        panes={option.key as PaneStyle}
        height={64}
      />
    </ChoiceCards>
  </section>
{/if}

<section class="group">
  <h3 class="label">{$t('settingsPanel.label.interfaceScale')}</h3>
  <Segments label={$t('settingsPanel.label.interfaceScale')} value={$prefs.uiScale} options={scales} mono on:change={(e) => setUIScale(e.detail)} />
</section>

<style>
  .group {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .group + .group {
    margin-top: var(--space-6);
  }

  .label {
    margin: 0;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  .hint {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .art {
    display: flex;
    align-items: center;
    justify-content: center;
    height: calc(var(--control-height) * 1.4);
    background: var(--surface-sunken);
  }

  .art.icon {
    color: var(--text-secondary);
  }

  .bars {
    flex-direction: column;
    align-items: stretch;
    padding: 0 var(--space-4);
  }

  .bars i {
    display: block;
    height: var(--space-1);
    border-radius: var(--space-1);
    background: var(--border-strong);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .bars i.short {
    width: 60%;
  }

  .times {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-4);
  }

  .time {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .time input {
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-family: var(--font-mono);
    font-size: var(--fz-label);
  }

  .swatches {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  .swatch {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--control-height);
    height: var(--control-height);
    padding: 0;
    border: none;
    border-radius: 50%;
    background: var(--sw);
    color: var(--accent-fg);
    cursor: var(--cursor-action);
    transition:
      box-shadow var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .swatch:active {
    transform: scale(0.9);
  }

  .swatch.on {
    box-shadow: 0 0 0 var(--space-1) var(--surface-base), 0 0 0 calc(var(--space-1) * 2) var(--sw, var(--accent));
  }

  /* the tick is drawn on the chosen swatch, as on the choice cards. */
  .swatch svg {
    width: 55%;
    height: 55%;
  }

  .swatch path {
    fill: none;
    stroke: var(--accent-fg);
    stroke-width: 1.8;
    stroke-linecap: round;
    stroke-linejoin: round;
    stroke-dasharray: 12;
    stroke-dashoffset: 12;
    transition: stroke-dashoffset var(--motion-base) var(--ease-out);
  }

  .swatch.on path {
    stroke-dashoffset: 0;
  }

  .swatch.custom {
    border: var(--hairline) dashed var(--border-strong);
    background: var(--surface-raised);
    color: var(--text-secondary);
  }

  .swatch.custom.on {
    border-style: solid;
    background: var(--accent);
    color: var(--accent-fg);
    box-shadow: 0 0 0 var(--space-1) var(--surface-base), 0 0 0 calc(var(--space-1) * 2) var(--accent);
  }

  .picker {
    align-self: flex-start;
    animation: pop-in var(--motion-base) var(--ease-out);
  }

  @keyframes pop-in {
    from {
      opacity: 0;
      transform: translateY(var(--space-2));
    }
  }
</style>
