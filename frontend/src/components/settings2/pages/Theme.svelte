<script lang="ts">
  // the Theme page: color mode and schedule, accent, installed themes, density,
  // corners, interface scale, window material, fonts and the reading pane image.
  import { onMount } from 'svelte'
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import ChoiceCards from '../ui/ChoiceCards.svelte'
  import ThemeSkeleton from '../skeletons/ThemeSkeleton.svelte'
  import DensitySkeleton from '../skeletons/DensitySkeleton.svelte'
  import CornerSkeleton from '../skeletons/CornerSkeleton.svelte'
  import Select from '../../common/Select.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import AccentPicker from '../../settings/AccentPicker.svelte'
  import ThemesSection from '../../settings/ThemesSection.svelte'
  import type { SelectItem } from '../../../lib/selectnav'
  import {
    prefs,
    setTheme,
    setThemeDarkTimes,
    setDensity,
    setCornerStyle,
    setUIScale,
    setWindowMaterial,
    setUIFont,
    setMonoFont,
    setEmptyStateImage,
    setEmptyStateFullscreen,
  } from '../../../stores/prefs'
  import { bodyFonts, uiFonts, monoFonts } from '../../../lib/fonts'
  import { listSystemFonts } from '../../../lib/api'
  import { toastError } from '../../../stores/toast'
  import { t, isMac, isWindows } from '../../../lib/i18n'
  import peltonLogo from '../../../assets/images/icons/pelton-logo.png'
  import type { ThemePref, DensityPref } from '../../../lib/types'

  // the native window material's product name, which is not translated. Linux
  // has none and gets no setting.
  const materialName = isMac ? 'Liquid Glass' : isWindows ? 'Mica' : ''

  type CornerKey = 'default' | 'square' | 'round'

  $: themeOptions = [
    { key: 'light', label: $t('onboarding.theme.light') },
    { key: 'dark', label: $t('onboarding.theme.dark') },
    { key: 'system', label: $t('onboarding.theme.system') },
    { key: 'schedule', label: $t('settingsPanel.theme.schedule') },
  ]

  $: densityOptions = [
    { key: 'compact', label: $t('onboarding.density.compact') },
    { key: 'medium', label: $t('onboarding.density.medium') },
    { key: 'luxe', label: $t('onboarding.density.luxe') },
  ]

  $: cornerOptions = [
    { key: 'default', label: $t('settingsPanel.corners.default') },
    { key: 'square', label: $t('settingsPanel.corners.square') },
    { key: 'round', label: $t('settingsPanel.corners.round') },
  ]

  $: scaleItems = [
    { value: '0.9', label: $t('settingsPanel.scale.90') },
    { value: '1', label: $t('settingsPanel.scale.100') },
    { value: '1.1', label: $t('settingsPanel.scale.110') },
    { value: '1.17', label: $t('settingsPanel.scale.117') },
    { value: '1.25', label: $t('settingsPanel.scale.125') },
    { value: '1.5', label: $t('settingsPanel.scale.150') },
  ]

  // the font pickers share a shape: a curated list, then whatever the system
  // has, prefixed so the backend can tell the two apart. loaded once; the list
  // is cached in the backend.
  let systemFonts: string[] = []
  onMount(() => {
    listSystemFonts()
      .then((fonts) => (systemFonts = fonts))
      .catch(() => (systemFonts = []))
  })

  function fontItems(list: typeof bodyFonts, installed: string[]): SelectItem[] {
    const curated = list.map((f) => ({ value: f.key, label: f.label ?? $t(f.labelKey ?? '') }))
    const items: SelectItem[] = [{ label: $t('settingsPanel.bodyFont.groupCurated'), options: curated }]
    if (installed.length > 0) {
      items.push({
        label: $t('settingsPanel.bodyFont.groupSystem'),
        options: installed.map((family) => ({ value: `sys:${family}`, label: family })),
      })
    }
    return items
  }

  $: uiFontItems = fontItems(uiFonts, systemFonts)
  $: monoFontItems = fontItems(monoFonts, systemFonts)

  // the reading-pane empty-state image is picked from a local file and stored as
  // a data uri. anything past the hard cap is refused; between the soft and hard
  // caps the user is warned but may proceed, since a large data uri in settings
  // can slow the ui down.
  let emptyImageInput: HTMLInputElement
  const maxEmptyImageBytes = 50_000_000
  const warnEmptyImageBytes = 3_000_000
  let dragonsPending: string | null = null

  function onPickEmptyImage(event: Event): void {
    const input = event.currentTarget as HTMLInputElement
    const file = input.files?.[0]
    input.value = ''
    if (!file) {
      return
    }
    if (file.size > maxEmptyImageBytes) {
      toastError($t('settingsPanel.error.imageTooLarge'))
      return
    }
    const large = file.size > warnEmptyImageBytes
    const reader = new FileReader()
    reader.onload = () => {
      const uri = String(reader.result)
      if (large) {
        dragonsPending = uri
      } else {
        setEmptyStateImage(uri)
      }
    }
    reader.onerror = () => toastError($t('settingsPanel.error.imageRead'))
    reader.readAsDataURL(file)
  }

  function confirmDragons(): void {
    if (dragonsPending) {
      setEmptyStateImage(dragonsPending)
    }
    dragonsPending = null
  }
</script>

<Card title={$t('settings2.theme.colors')}>
  {#if $prefs.themeId}
    <Row id="theme-mode" label={$t('settingsPanel.label.theme')} hint={$t('themes.baseLockedHint')} />
  {:else}
    <Row id="theme-mode" label={$t('settingsPanel.label.theme')} stacked>
      <ChoiceCards
        label={$t('settingsPanel.label.theme')}
        value={$prefs.theme}
        options={themeOptions}
        minWidth={120}
        on:change={(e) => setTheme(e.detail as ThemePref)}
        let:option
      >
        <ThemeSkeleton mode={option.key as ThemePref} />
      </ChoiceCards>
    </Row>
    {#if $prefs.theme === 'schedule'}
      <Row id="theme-schedule" label={$t('settingsPanel.theme.schedule')} hint={$t('settingsPanel.hint.themeSchedule')}>
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
      </Row>
    {/if}
  {/if}
  <div class="plain" data-setting="accent">
    <AccentPicker />
  </div>
</Card>

<Card title={$t('settings2.theme.custom')}>
  <Row id="themes" label={$t('settingsPanel.category.themes')} stacked>
    <ThemesSection />
  </Row>
</Card>

<Card title={$t('settings2.theme.shape')}>
  <Row id="density" label={$t('settingsPanel.label.density')} stacked>
    <ChoiceCards
      label={$t('settingsPanel.label.density')}
      value={$prefs.density}
      options={densityOptions}
      minWidth={120}
      on:change={(e) => setDensity(e.detail as DensityPref)}
      let:option
    >
      <DensitySkeleton density={option.key as DensityPref} />
    </ChoiceCards>
  </Row>
  <Row id="corners" label={$t('settingsPanel.label.corners')} hint={$t('settingsPanel.hint.corners')} stacked>
    <ChoiceCards
      label={$t('settingsPanel.label.corners')}
      value={$prefs.cornerStyle}
      options={cornerOptions}
      minWidth={120}
      on:change={(e) => setCornerStyle(e.detail)}
      let:option
    >
      <CornerSkeleton corners={option.key as CornerKey} />
    </ChoiceCards>
  </Row>
  <Row id="interface-scale" label={$t('settingsPanel.label.interfaceScale')} hint={$t('settingsPanel.hint.interfaceScale')}>
    <Select
      value={$prefs.uiScale}
      ariaLabel={$t('settingsPanel.label.interfaceScale')}
      items={scaleItems}
      on:change={(e) => setUIScale(e.detail)}
    />
  </Row>
</Card>

<Card title={$t('settings2.theme.fonts')}>
  <Row id="ui-font" label={$t('settingsPanel.label.uiFont')} hint={$t('settingsPanel.hint.uiFont')}>
    <Select value={$prefs.uiFont} ariaLabel={$t('settingsPanel.label.uiFont')} items={uiFontItems} on:change={(e) => setUIFont(e.detail)} />
  </Row>
  <Row id="mono-font" label={$t('settingsPanel.label.monoFont')} hint={$t('settingsPanel.hint.monoFont')}>
    <Select
      value={$prefs.monoFont}
      ariaLabel={$t('settingsPanel.label.monoFont')}
      items={monoFontItems}
      on:change={(e) => setMonoFont(e.detail)}
    />
  </Row>
</Card>

<Card title={$t('settings2.theme.window')}>
  {#if materialName}
    <Row id="window-material" label={materialName} hint={$t('settingsPanel.hint.windowMaterial')}>
      <ToggleSwitch checked={$prefs.windowMaterial} label={materialName} on:change={(e) => setWindowMaterial(e.detail)} />
    </Row>
  {/if}
  <Row id="empty-image" label={$t('settingsPanel.label.emptyStateImage')} hint={$t('settingsPanel.hint.emptyStateImage')}>
    <span class="thumb"><img src={$prefs.emptyStateImage || peltonLogo} alt="" draggable="false" /></span>
    <button type="button" class="action" on:click={() => emptyImageInput?.click()}>{$t('settingsPanel.button.selectImage')}</button>
    <button type="button" class="action" disabled={!$prefs.emptyStateImage} on:click={() => setEmptyStateImage('')}>
      {$t('settingsPanel.button.resetImage')}
    </button>
    <input class="file" type="file" accept="image/*" bind:this={emptyImageInput} on:change={onPickEmptyImage} />
  </Row>
  {#if dragonsPending}
    <div class="warn">
      <p>{$t('settingsPanel.warn.imageLarge')}</p>
      <div class="warn-actions">
        <button type="button" class="action" on:click={() => (dragonsPending = null)}>{$t('settingsPanel.button.cancel')}</button>
        <button type="button" class="action danger" on:click={confirmDragons}>{$t('settingsPanel.button.useAnyway')}</button>
      </div>
    </div>
  {/if}
  <Row
    id="empty-fullscreen"
    label={$t('settingsPanel.toggle.emptyStateFullscreen')}
    hint={$t('settingsPanel.hint.emptyStateFullscreen')}
    disabled={!$prefs.emptyStateImage}
  >
    <ToggleSwitch
      checked={$prefs.emptyStateFullscreen}
      disabled={!$prefs.emptyStateImage}
      label={$t('settingsPanel.toggle.emptyStateFullscreen')}
      on:change={(e) => setEmptyStateFullscreen(e.detail)}
    />
  </Row>
</Card>

<style>
  .plain {
    padding: var(--space-3) var(--space-4);
  }

  .time {
    display: inline-flex;
    align-items: center;
    gap: var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .time input {
    min-height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-base);
    font-family: inherit;
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .thumb {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: calc(var(--control-height) * 2);
    height: var(--control-height);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    overflow: hidden;
  }

  .thumb img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
  }

  .file {
    display: none;
  }

  .action {
    min-height: var(--control-height);
    padding: 0 var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-size: var(--fz-label);
    color: var(--text-primary);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .action:hover:not(:disabled) {
    background: var(--surface-hover);
  }

  .action:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .action.danger {
    border-color: var(--danger);
    color: var(--danger);
  }

  .warn {
    padding: var(--space-3) var(--space-4);
    border-inline-start: 2px solid var(--warning);
    background: var(--warning-bg);
  }

  .warn p {
    margin: 0 0 var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .warn-actions {
    display: flex;
    gap: var(--space-3);
  }
</style>
