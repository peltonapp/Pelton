<script lang="ts">
  // the first-run onboarding. it is a deliberately unhurried, full-screen flow:
  // a welcome, a few honest selling points, then theme / accent / density chosen
  // with live previews (every pick applies to the whole app immediately), then
  // adding a mailbox or skipping, and finally a celebratory "all set". a skip
  // affordance is always available. completion is persisted by the parent so this
  // only ever shows once, with a settings action to run it again.
  import { createEventDispatcher, onMount } from 'svelte'
  import { fade, scale } from 'svelte/transition'
  import { backOut } from 'svelte/easing'
  import {
    IconArrowRight,
    IconArrowLeft,
    IconCheck,
    IconDeviceDesktop,
    IconSun,
    IconMoon,
    IconShieldLock,
    IconBolt,
    IconAdjustmentsHorizontal,
    IconPackageExport,
    IconBrandOpenSource,
    IconSparkles,
    IconPalette,
    IconWorld,
    IconMail,
    IconMailbox,
    IconPlus,
  } from '@tabler/icons-svelte'
  import Confetti from '../common/Confetti.svelte'
  import AppSkeleton from './AppSkeleton.svelte'
  import ColorPicker from '../common/ColorPicker.svelte'
  import AddMailboxWizard from '../wizard/AddMailboxWizard.svelte'
  import RowLayoutPreview from '../settings/RowLayoutPreview.svelte'
  import SegmentedSetting from '../settings/SegmentedSetting.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import VimPreview from './VimPreview.svelte'
  import LanguageSelect from '../common/LanguageSelect.svelte'
  import LiabilityNotice from '../common/LiabilityNotice.svelte'
  import {
    prefs,
    setTheme,
    setDensity,
    setAccent,
    setUIScale,
    setRowTemplate,
    setComposeVimMode,
    setAppVimMode,
    setAvatarStyle,
    setAvatarSource,
    setAlwaysLoadImages,
    setBlockTrackingPixels,
    setMessageFontSize,
    setFlagHighlight,
    setLanguage,
    setLowPowerMode,
  } from '../../stores/prefs'
  import { ACCENT_PRESETS } from '../../theme/accent'
  import { defaultMailClientStatus, setDefaultMailClient } from '../../lib/api'
  import { acceptLiability } from '../../lib/liability'
  import { toastError, errorMessage } from '../../stores/toast'
  import { pfpDataUri, type PfpStyle } from '../../lib/pfp'
  import { initials } from '../../lib/format'
  import { shortcutLabel, t } from '../../lib/i18n'
  import logo from '../../assets/images/icons/pelton-logo.png'
  import type { ThemePref, DensityPref, Account } from '../../lib/types'

  const dispatch = createEventDispatcher<{ finish: void; added: Account }>()
  $: currentLocale = $prefs.language

  // labels for choice cards are translated reactively so they follow the
  // language step's live pick, not just the persisted setting.
  $: themeChoices = [
    { key: 'system' as ThemePref, label: $t('onboarding.theme.system'), icon: IconDeviceDesktop },
    { key: 'light' as ThemePref, label: $t('onboarding.theme.light'), icon: IconSun },
    { key: 'dark' as ThemePref, label: $t('onboarding.theme.dark'), icon: IconMoon },
  ]
  $: densityChoices = [
    { key: 'compact' as DensityPref, label: $t('onboarding.density.compact'), gap: 'var(--space-1)' },
    { key: 'medium' as DensityPref, label: $t('onboarding.density.medium'), gap: 'var(--space-2)' },
    { key: 'luxe' as DensityPref, label: $t('onboarding.density.luxe'), gap: 'var(--space-3)' },
  ]
  $: rowTemplateChoices = [
    { key: 'relaxed', label: $t('onboarding.row.relaxed') },
    { key: 'comfortable', label: $t('onboarding.row.comfortable') },
    { key: 'compact', label: $t('onboarding.row.compact') },
    { key: 'single', label: $t('onboarding.row.single') },
  ]
  $: avatarStyleChoices = [
    { key: 'initials' as PfpStyle, label: $t('onboarding.avatar.classic') },
    { key: 'mono' as PfpStyle, label: $t('onboarding.avatar.mono') },
    { key: 'pixel' as PfpStyle, label: $t('onboarding.avatar.pixel') },
    { key: 'geometric' as PfpStyle, label: $t('onboarding.avatar.geometric') },
  ]
  $: fontOptions = [
    { key: '12', label: $t('onboarding.font.small') },
    { key: '14', label: $t('onboarding.font.default') },
    { key: '16', label: $t('onboarding.font.large') },
    { key: '18', label: $t('onboarding.font.larger') },
    { key: '20', label: $t('onboarding.font.largest') },
  ]
  $: flagOptions = [
    { key: 'flag', label: $t('onboarding.flagopt.icon') },
    { key: 'left', label: $t('onboarding.flagopt.left') },
    { key: 'both', label: $t('onboarding.flagopt.both') },
    { key: 'off', label: $t('onboarding.flagopt.off') },
  ]
  $: features = [
    { icon: IconShieldLock, title: $t('onboarding.feature.privacy.title'), body: $t('onboarding.feature.privacy.body') },
    { icon: IconBolt, title: $t('onboarding.feature.search.title'), body: $t('onboarding.feature.search.body') },
    { icon: IconAdjustmentsHorizontal, title: $t('onboarding.feature.customizable.title'), body: $t('onboarding.feature.customizable.body') },
    { icon: IconPackageExport, title: $t('onboarding.feature.portable.title'), body: $t('onboarding.feature.portable.body') },
    { icon: IconBrandOpenSource, title: $t('onboarding.feature.foss.title'), body: $t('onboarding.feature.foss.body') },
  ]
  // privacyMode is derived straight from the two settings it controls, not a
  // separate stored field, so it always reflects what's actually active (and
  // so re-running onboarding shows the real current choice, not a guess).
  $: privacyMode =
    $prefs.avatarSource === 'pfp' && !$prefs.alwaysLoadImages && $prefs.blockTrackingPixels ? 'private' : 'normal'

  function selectPrivacy(mode: 'private' | 'normal'): void {
    if (mode === 'private') {
      setAvatarSource('pfp')
      setAlwaysLoadImages(false)
      setBlockTrackingPixels(true)
    } else {
      setAvatarSource('bimi_gravatar')
      setAlwaysLoadImages(true)
      setBlockTrackingPixels(false)
    }
  }

  $: privacyChoices = [
    {
      key: 'private' as const,
      icon: IconShieldLock,
      label: $t('onboarding.privacy.private.label'),
      recommended: true,
      body: $t('onboarding.privacy.private.body'),
    },
    {
      key: 'normal' as const,
      icon: IconWorld,
      label: $t('onboarding.privacy.normal.label'),
      recommended: false,
      body: $t('onboarding.privacy.normal.body'),
    },
  ]

  // the ordered steps. "done" is the finale and has no skip/progress chrome.
  type Step =
    | 'welcome'
    | 'liability'
    | 'language'
    | 'features'
    | 'privacy'
    | 'theme'
    | 'accent'
    | 'density'
    | 'scale'
    | 'layout'
    | 'extras'
    | 'defaultmail'
    | 'mailbox'
    | 'done'
  const baseOrder: Step[] = [
    'welcome',
    'liability',
    'language',
    'features',
    'privacy',
    'theme',
    'accent',
    'density',
    'scale',
    'layout',
    'extras',
    'defaultmail',
    'mailbox',
    'done',
  ]
  // whether Pelton is already the default mail client. the offer step is dropped
  // when it is (nothing to offer) or where the platform cannot answer at all,
  // matching the quiet-by-design intent. resolved once at mount, before the user
  // advances, so reordering never shifts a step out from under them.
  let offerDefaultMail = false
  $: order = offerDefaultMail ? baseOrder : baseOrder.filter((s) => s !== 'defaultmail')
  let index = 0
  $: step = order[index]

  onMount(async () => {
    try {
      const s = await defaultMailClientStatus()
      offerDefaultMail = s.known && !s.isDefault
    } catch {
      offerDefaultMail = false
    }
  })

  // makeDefault takes the offer, then advances. macOS shows its own
  // confirmation sheet; a failure surfaces quietly and does not block the flow.
  let settingDefault = false
  async function makeDefault(): Promise<void> {
    settingDefault = true
    try {
      await setDefaultMailClient()
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      settingDefault = false
      next()
    }
  }

  let showWizard = false
  let skippedMailbox = false

  function next(): void {
    index = Math.min(index + 1, order.length - 1)
  }

  // the liability acknowledgement is persisted the moment it is given, not at
  // the end of the flow, so quitting mid-onboarding does not lose it.
  let liabilityAccepted = false
  async function confirmLiability(): Promise<void> {
    if (!liabilityAccepted) return
    try {
      await acceptLiability()
    } catch (err) {
      toastError(errorMessage(err))
    }
    next()
  }

  function back(): void {
    index = Math.max(index - 1, 0)
  }

  function finish(): void {
    dispatch('finish')
  }

  // each density card illustrates its spacing with bars set apart by a real
  // spacing token, so the comparison is honest and stays theme-driven. selecting
  // also applies the density to the whole app live.

  // interface zoom choices, shown live so the whole onboarding scales as you pick.
  const scaleChoices: { key: string; label: string }[] = [
    { key: '0.9', label: '90%' },
    { key: '1', label: '100%' },
    { key: '1.1', label: '110%' },
    { key: '1.17', label: '117%' },
    { key: '1.25', label: '125%' },
    { key: '1.5', label: '150%' },
  ]

  // the extras step: a few settings with a live preview on the right that follows
  // whichever group the user hovers. previews are computed from the current value.
  type ExtraKey = 'vim' | 'appvim' | 'avatar' | 'fontsize' | 'flag' | 'lowpower'
  let hovered: ExtraKey = 'vim'

  // string-keyed options so the shared SegmentedSetting can drive them.
  const sampleEmail = 'potato@pelton.email'
  const sampleInitials = initials('', sampleEmail)
  function stylePreview(style: PfpStyle): string {
    return pfpDataUri(style, sampleEmail, sampleInitials)
  }
  $: avatarPreview = pfpDataUri($prefs.avatarStyle as PfpStyle, sampleEmail, sampleInitials)

  const addMailboxHint = shortcutLabel('mod+m')

  // custom accent: a popover color picker (hue square + slider + hex), opened from
  // the custom swatch. it applies live through the same accent setter.
  let pickerOpen = false
  $: isPresetAccent = ACCENT_PRESETS.some((c) => c.toLowerCase() === $prefs.accent.toLowerCase())

  function onMailboxAdded(event: CustomEvent<Account>): void {
    showWizard = false
    dispatch('added', event.detail)
    index = order.indexOf('done')
  }

  // both choices open the same dialogs the rest of the app uses, so there is one
  // flow of each to maintain. They open over the onboarding screen and close
  // back to it.
  let showImport = false

  function skipMailbox(): void {
    skippedMailbox = true
    next()
  }
</script>

<div class="screen" role="dialog" aria-modal="true" aria-label="Welcome to Pelton">
  <!-- no skip before the acknowledgement is given: it is the one thing the flow
       does not let you walk past, and skipping from the welcome screen would
       jump straight over it. -->
  {#if step !== 'done' && step !== 'welcome' && step !== 'liability'}
    <button type="button" class="skip" on:click={finish}>{$t('onboarding.skip')}</button>
  {/if}

  <div class="stage" class:wide={step === 'extras'}>
    {#key step}
      <!-- fade only, and no out transition: the previous step leaves at once so
           the stage resizes while the new one is still transparent, instead of
           shifting the content under the pointer part way through. -->
      <div class="step" in:fade={{ duration: 120 }}>
        {#if step === 'welcome'}
          <div class="welcome">
            <img class="logo" src={logo} alt="Pelton" in:scale={{ start: 0.7, duration: 600, easing: backOut }} />
            <h1>{$t('onboarding.welcome')}</h1>
            <p class="lede">
              {$t('onboarding.welcomeLede')}
            </p>
            <button class="primary big" on:click={next}>
              {$t('onboarding.getStarted')} <IconArrowRight size={18} stroke={1.8} />
            </button>
          </div>
        {:else if step === 'liability'}
          <div class="choose liability">
            <h2>{$t('liability.title')}</h2>
            <LiabilityNotice bind:accepted={liabilityAccepted} />
            <div class="nav">
              <button class="ghost" on:click={back}><IconArrowLeft size={16} stroke={1.8} /> {$t('onboarding.back')}</button>
              <button class="primary" disabled={!liabilityAccepted} on:click={confirmLiability}>
                {$t('onboarding.continue')} <IconArrowRight size={16} stroke={1.8} />
              </button>
            </div>
          </div>
        {:else if step === 'language'}
          <div class="choose">
            <h2>{$t('onboarding.language')}</h2>
            <p class="sub">{$t('onboarding.languageSub')}</p>
            <LanguageSelect value={currentLocale} onSelect={setLanguage} />
            <div class="nav">
              <button class="ghost" on:click={back}><IconArrowLeft size={16} stroke={1.8} /> {$t('onboarding.back')}</button>
              <button class="primary" on:click={next}>{$t('onboarding.continue')} <IconArrowRight size={16} stroke={1.8} /></button>
            </div>
          </div>
        {:else if step === 'features'}
          <div class="features">
            <h2>{$t('onboarding.featuresTitle')}</h2>
            <ul>
              {#each features as f}
                <li>
                  <span class="f-icon"><svelte:component this={f.icon} size={22} stroke={1.6} /></span>
                  <span class="f-text">
                    <span class="f-title">{f.title}</span>
                    <span class="f-body">{f.body}</span>
                  </span>
                </li>
              {/each}
            </ul>
            <div class="nav">
              <button class="ghost" on:click={back}><IconArrowLeft size={16} stroke={1.8} /> {$t('onboarding.back')}</button>
              <button class="primary" on:click={next}>{$t('onboarding.continue')} <IconArrowRight size={16} stroke={1.8} /></button>
            </div>
          </div>
        {:else if step === 'privacy'}
          <div class="choose">
            <h2>{$t('onboarding.privacyTitle')}</h2>
            <p class="sub">{$t('onboarding.privacySub')}</p>
            <div class="privacy-cards">
              {#each privacyChoices as c}
                <button class="privacy-card" class:active={privacyMode === c.key} on:click={() => selectPrivacy(c.key)}>
                  <span class="pc-head">
                    <span class="pc-icon"><svelte:component this={c.icon} size={22} stroke={1.6} /></span>
                    <span class="pc-label">{c.label}</span>
                    {#if c.recommended}<span class="pc-recommended">{$t('onboarding.privacy.recommended')}</span>{/if}
                  </span>
                  <span class="pc-body">{c.body}</span>
                  {#if privacyMode === c.key}<span class="tick"><IconCheck size={14} stroke={2.4} /></span>{/if}
                </button>
              {/each}
            </div>
            <div class="nav">
              <button class="ghost" on:click={back}><IconArrowLeft size={16} stroke={1.8} /> {$t('onboarding.back')}</button>
              <button class="primary" on:click={next}>{$t('onboarding.continue')} <IconArrowRight size={16} stroke={1.8} /></button>
            </div>
          </div>
        {:else if step === 'theme'}
          <div class="choose">
            <h2>{$t('onboarding.themeTitle')}</h2>
            <div class="cards three">
              {#each themeChoices as c}
                <button class="card" class:active={$prefs.theme === c.key} on:click={() => setTheme(c.key)}>
                  <span class="card-icon"><svelte:component this={c.icon} size={26} stroke={1.5} /></span>
                  <span class="card-label">{c.label}</span>
                  {#if $prefs.theme === c.key}<span class="tick"><IconCheck size={14} stroke={2.4} /></span>{/if}
                </button>
              {/each}
            </div>
            <div class="nav">
              <button class="ghost" on:click={back}><IconArrowLeft size={16} stroke={1.8} /> {$t('onboarding.back')}</button>
              <button class="primary" on:click={next}>{$t('onboarding.continue')} <IconArrowRight size={16} stroke={1.8} /></button>
            </div>
          </div>
        {:else if step === 'accent'}
          <div class="choose">
            <h2>{$t('onboarding.accentTitle')}</h2>
            <AppSkeleton />
            <div class="swatches">
              {#each ACCENT_PRESETS as color}
                <button
                  class="swatch"
                  class:active={$prefs.accent.toLowerCase() === color.toLowerCase()}
                  style={`--sw:${color}`}
                  aria-label={`Accent ${color}`}
                  on:click={() => setAccent(color)}
                >
                  {#if $prefs.accent.toLowerCase() === color.toLowerCase()}
                    <IconCheck size={16} stroke={2.6} />
                  {/if}
                </button>
              {/each}
              <button
                type="button"
                class="custom-swatch"
                class:active={!isPresetAccent}
                title={$t('onboarding.customColor')}
                aria-label={$t('onboarding.customColor')}
                on:click={() => (pickerOpen = !pickerOpen)}
              >
                <IconPalette size={16} stroke={1.8} />
              </button>
            </div>
            {#if pickerOpen}
              <div class="picker-wrap" transition:fade={{ duration: 120 }}>
                <ColorPicker value={$prefs.accent} on:change={(e) => setAccent(e.detail)} />
              </div>
            {/if}
            <div class="nav">
              <button class="ghost" on:click={back}><IconArrowLeft size={16} stroke={1.8} /> {$t('onboarding.back')}</button>
              <button class="primary" on:click={next}>{$t('onboarding.continue')} <IconArrowRight size={16} stroke={1.8} /></button>
            </div>
          </div>
        {:else if step === 'density'}
          <div class="choose">
            <h2>{$t('onboarding.densityTitle')}</h2>
            <AppSkeleton />
            <div class="cards three dense">
              {#each densityChoices as c}
                <button class="card" class:active={$prefs.density === c.key} on:click={() => setDensity(c.key)}>
                  <span class="bars" style={`gap:${c.gap}`}>
                    <span class="bar"></span><span class="bar short"></span><span class="bar"></span>
                  </span>
                  <span class="card-label">{c.label}</span>
                  {#if $prefs.density === c.key}<span class="tick"><IconCheck size={14} stroke={2.4} /></span>{/if}
                </button>
              {/each}
            </div>
            <div class="nav">
              <button class="ghost" on:click={back}><IconArrowLeft size={16} stroke={1.8} /> {$t('onboarding.back')}</button>
              <button class="primary" on:click={next}>{$t('onboarding.continue')} <IconArrowRight size={16} stroke={1.8} /></button>
            </div>
          </div>
        {:else if step === 'scale'}
          <div class="choose">
            <h2>{$t('onboarding.scaleTitle')}</h2>
            <p class="sub">{$t('onboarding.scaleSub')}</p>
            <AppSkeleton />
            <div class="cards five">
              {#each scaleChoices as c}
                <button class="card" class:active={$prefs.uiScale === c.key} on:click={() => setUIScale(c.key)}>
                  <span class="card-label">{c.label}</span>
                  {#if $prefs.uiScale === c.key}<span class="tick"><IconCheck size={14} stroke={2.4} /></span>{/if}
                </button>
              {/each}
            </div>
            <div class="nav">
              <button class="ghost" on:click={back}><IconArrowLeft size={16} stroke={1.8} /> {$t('onboarding.back')}</button>
              <button class="primary" on:click={next}>{$t('onboarding.continue')} <IconArrowRight size={16} stroke={1.8} /></button>
            </div>
          </div>
        {:else if step === 'layout'}
          <div class="choose">
            <h2>{$t('onboarding.layoutTitle')}</h2>
            <p class="sub">{$t('onboarding.layoutSub')}</p>
            <div class="cards four">
              {#each rowTemplateChoices as c}
                <button class="card" class:active={$prefs.rowTemplate === c.key} on:click={() => setRowTemplate(c.key)}>
                  <span class="card-label">{c.label}</span>
                  {#if $prefs.rowTemplate === c.key}<span class="tick"><IconCheck size={14} stroke={2.4} /></span>{/if}
                </button>
              {/each}
            </div>
            <RowLayoutPreview />
            <div class="nav">
              <button class="ghost" on:click={back}><IconArrowLeft size={16} stroke={1.8} /> {$t('onboarding.back')}</button>
              <button class="primary" on:click={next}>{$t('onboarding.continue')} <IconArrowRight size={16} stroke={1.8} /></button>
            </div>
          </div>
        {:else if step === 'extras'}
          <div class="choose wide">
            <h2>{$t('onboarding.extrasTitle')}</h2>
            <p class="sub">{$t('onboarding.extrasSub')}</p>
            <div class="extras">
              <div class="extras-settings">
                <!-- svelte-ignore a11y-no-static-element-interactions a11y-mouse-events-have-key-events -->
                <div class="ex-group" on:mouseenter={() => (hovered = 'vim')}>
                  <div class="ex-toggle">
                    <span class="ex-label">{$t('onboarding.extras.vimEditor')} <span class="badge-experimental">{$t('common.experimental')}</span></span>
                    <ToggleSwitch
                      checked={$prefs.composeVimMode}
                      label={$t('onboarding.extras.vimEditor')}
                      on:change={(e) => setComposeVimMode(e.detail)}
                    />
                  </div>
                </div>
                <!-- svelte-ignore a11y-no-static-element-interactions a11y-mouse-events-have-key-events -->
                <div class="ex-group" on:mouseenter={() => (hovered = 'appvim')}>
                  <div class="ex-toggle">
                    <span class="ex-label">{$t('onboarding.extras.appVim')} <span class="badge-experimental">{$t('common.experimental')}</span></span>
                    <ToggleSwitch
                      checked={$prefs.appVimMode}
                      label={$t('onboarding.extras.appVim')}
                      on:change={(e) => setAppVimMode(e.detail)}
                    />
                  </div>
                </div>
                <!-- svelte-ignore a11y-no-static-element-interactions a11y-mouse-events-have-key-events -->
                <div class="ex-group" on:mouseenter={() => (hovered = 'avatar')}>
                  <span class="ex-label">{$t('onboarding.extras.avatarStyle')}</span>
                  <div class="style-grid">
                    {#each avatarStyleChoices as opt (opt.key)}
                      <button
                        type="button"
                        class="style-card"
                        class:active={$prefs.avatarStyle === opt.key}
                        on:click={() => setAvatarStyle(opt.key)}
                      >
                        <img class="style-img" src={stylePreview(opt.key)} alt="" aria-hidden="true" />
                        <span class="style-label">{opt.label}</span>
                      </button>
                    {/each}
                  </div>
                </div>
                <!-- svelte-ignore a11y-no-static-element-interactions a11y-mouse-events-have-key-events -->
                <div class="ex-group" on:mouseenter={() => (hovered = 'fontsize')}>
                  <SegmentedSetting
                    label={$t('onboarding.extras.fontSize')}
                    value={String($prefs.messageFontSize)}
                    options={fontOptions}
                    on:change={(e) => setMessageFontSize(Number(e.detail))}
                  />
                </div>
                <!-- svelte-ignore a11y-no-static-element-interactions a11y-mouse-events-have-key-events -->
                <div class="ex-group" on:mouseenter={() => (hovered = 'flag')}>
                  <SegmentedSetting
                    label={$t('onboarding.extras.flagHighlight')}
                    value={$prefs.flagHighlight}
                    options={flagOptions}
                    on:change={(e) => setFlagHighlight(e.detail)}
                  />
                </div>
                <!-- svelte-ignore a11y-no-static-element-interactions a11y-mouse-events-have-key-events -->
                <div class="ex-group" on:mouseenter={() => (hovered = 'lowpower')}>
                  <div class="ex-toggle">
                    <span class="ex-label">{$t('onboarding.extras.lowPower')}</span>
                    <ToggleSwitch
                      checked={$prefs.lowPowerMode}
                      label={$t('onboarding.extras.lowPower')}
                      on:change={(e) => setLowPowerMode(e.detail)}
                    />
                  </div>
                </div>
              </div>

              <div class="extras-preview" aria-hidden="true">
                {#if hovered === 'vim'}
                  <VimPreview enabled={$prefs.composeVimMode} />
                {:else if hovered === 'appvim'}
                  <div class="pv-center">
                    <p class="pv-sample">
                      {#if $prefs.appVimMode}
                        {$t('onboarding.preview.appVimOn')}
                      {:else}
                        {$t('onboarding.preview.appVimOff')}
                      {/if}
                    </p>
                  </div>
                {:else if hovered === 'avatar'}
                  <div class="pv-center">
                    <img class="pv-avatar" src={avatarPreview} alt="" />
                    <span class="pv-cap">{sampleEmail}</span>
                  </div>
                {:else if hovered === 'fontsize'}
                  <div class="pv-center">
                    <p class="pv-sample" style={`font-size:${$prefs.messageFontSize}px`}>
                      {$t('onboarding.preview.pangram')}
                    </p>
                  </div>
                {:else if hovered === 'flag'}
                  <div class="pv-rows">
                    <div class="pv-row" class:bar={$prefs.flagHighlight === 'left' || $prefs.flagHighlight === 'both'}>
                      <span class="pv-from">Ada Lovelace</span>
                      {#if $prefs.flagHighlight === 'flag' || $prefs.flagHighlight === 'both'}<span class="pv-flag">⚑</span>{/if}
                      <span class="pv-time">9:24</span>
                    </div>
                  </div>
                {:else if hovered === 'lowpower'}
                  <div class="pv-center">
                    <p class="pv-sample">
                      {#if $prefs.lowPowerMode}
                        {$t('onboarding.preview.lowPowerOn')}
                      {:else}
                        {$t('onboarding.preview.lowPowerOff')}
                      {/if}
                    </p>
                  </div>
                {/if}
              </div>
            </div>

            <div class="nav">
              <button class="ghost" on:click={back}><IconArrowLeft size={16} stroke={1.8} /> {$t('onboarding.back')}</button>
              <button class="primary" on:click={next}>{$t('onboarding.continue')} <IconArrowRight size={16} stroke={1.8} /></button>
            </div>
          </div>
        {:else if step === 'defaultmail'}
          <div class="choose">
            <h2>{$t('onboarding.defaultMailTitle')}</h2>
            <p class="sub">{$t('onboarding.defaultMailSub')}</p>
            <div class="default-mail">
              <span class="dm-icon"><IconMail size={40} stroke={1.4} /></span>
              <button type="button" class="dm-cta" disabled={settingDefault} on:click={makeDefault}>
                {$t('onboarding.defaultMailSet')}
              </button>
            </div>
            <div class="nav">
              <button class="ghost" on:click={back}><IconArrowLeft size={16} stroke={1.8} /> {$t('onboarding.back')}</button>
              <button class="primary" on:click={next}>{$t('onboarding.skipForNow')}</button>
            </div>
          </div>
        {:else if step === 'mailbox'}
          <div class="choose">
            <h2>{$t('onboarding.mailboxTitle')}</h2>
            <p class="sub">{$t('onboarding.mailboxSub')}</p>
            <div class="providers">
              <button class="provider" on:click={() => (showWizard = true)}>
                <span class="p-icon"><IconPlus size={24} stroke={1.5} /></span>
                <span class="p-text">
                  <span class="p-title">{$t('wizard.start.addTitle')}</span>
                  <span class="p-sub">{$t('wizard.start.addSub')}</span>
                </span>
                <IconArrowRight size={16} stroke={1.8} />
              </button>
              <button class="provider" on:click={() => (showImport = true)}>
                <span class="p-icon"><IconMailbox size={24} stroke={1.5} /></span>
                <span class="p-text">
                  <span class="p-title">{$t('wizard.start.importTitle')}</span>
                  <span class="p-sub">{$t('wizard.start.importSub')}</span>
                </span>
                <IconArrowRight size={16} stroke={1.8} />
              </button>
            </div>
            <div class="nav">
              <button class="ghost" on:click={back}><IconArrowLeft size={16} stroke={1.8} /> {$t('onboarding.back')}</button>
              <button class="primary" on:click={skipMailbox}>{$t('onboarding.skipForNow')}</button>
            </div>
          </div>
        {:else if step === 'done'}
          <div class="welcome done">
            <span class="done-mark" in:scale={{ start: 0.5, duration: 600, easing: backOut }}>
              <IconSparkles size={40} stroke={1.5} />
            </span>
            <h1>{$t('onboarding.doneTitle')}</h1>
            <p class="lede">
              {#if skippedMailbox}
                {$t('onboarding.doneLedeSkippedBefore')} <kbd>{addMailboxHint}</kbd> {$t('onboarding.doneLedeSkippedAfter')}
              {:else}
                {$t('onboarding.doneLedeDefault')}
              {/if}
            </p>
            <button class="primary big" on:click={finish}>
              {$t('onboarding.startUsing')}
            </button>
          </div>
        {/if}
      </div>
    {/key}
  </div>

  {#if step !== 'done'}
    <div class="dots" aria-hidden="true">
      {#each order.slice(0, -1) as _, i}
        <span class="dot" class:on={i === index}></span>
      {/each}
    </div>
  {/if}
</div>

{#if step === 'done'}
  <Confetti />
{/if}

{#if showImport}
  {#await import('../settings/ImportThunderbirdModal.svelte') then m}
    <svelte:component this={m.default} on:close={() => (showImport = false)} />
  {/await}
{/if}

{#if showWizard}
  <AddMailboxWizard
    offerImport={false}
    skipFolderPicker
    on:close={() => (showWizard = false)}
    on:added={onMailboxAdded}
  />
{/if}

<style>
  .screen {
    position: fixed;
    inset: 0;
    z-index: 120;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    background: var(--surface-base);
    overflow: hidden;
    /* covers the whole window, so it has to keep the macOS traffic lights clear
       itself; zero on every other platform. */
    padding-top: var(--titlebar-lights);
  }

  .skip {
    position: absolute;
    top: var(--space-5);
    right: var(--space-6);
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-control);
  }

  .skip:hover {
    background: var(--surface-hover);
    color: var(--text-secondary);
  }

  .stage {
    width: 100%;
    max-width: 560px;
    padding: 0 var(--space-6);
    display: grid;
  }

  /* the extras step needs more room for its two columns. */
  .stage.wide {
    max-width: 760px;
  }

  /* every step occupies the same grid cell so one fades in exactly where the
     last one was. */
  .step {
    grid-area: 1 / 1;
  }

  h1 {
    margin: var(--space-4) 0 0;
    font-size: 2rem;
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
    letter-spacing: -0.02em;
  }

  h2 {
    margin: 0 0 var(--space-2);
    font-size: var(--fz-title);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
    text-align: center;
  }

  .sub,
  .lede {
    color: var(--text-secondary);
    line-height: 1.55;
  }

  .sub {
    margin: 0 0 var(--space-5);
    font-size: var(--fz-label);
    text-align: center;
  }

  /* welcome + done are centered hero layouts. */
  .welcome {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
  }

  .logo {
    width: 96px;
    height: 96px;
    object-fit: contain;
    filter: drop-shadow(0 8px 24px rgba(0, 0, 0, 0.18));
  }

  .lede {
    margin: var(--space-3) 0 var(--space-6);
    font-size: var(--fz-body);
    max-width: 42ch;
  }

  .done-mark {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 84px;
    height: 84px;
    border-radius: 999px;
    background: var(--surface-sunken);
    color: var(--accent);
    border: var(--hairline) solid var(--border-subtle);
  }

  kbd {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    padding: 1px var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
  }

  /* features list. */
  .features ul {
    list-style: none;
    margin: var(--space-5) 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .features li {
    display: flex;
    gap: var(--space-4);
    align-items: flex-start;
  }

  .f-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 42px;
    height: 42px;
    flex-shrink: 0;
    border-radius: var(--radius-card);
    background: var(--surface-sunken);
    color: var(--accent);
    border: var(--hairline) solid var(--border-subtle);
  }

  .f-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .f-title {
    font-size: var(--fz-body);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .f-body {
    font-size: var(--fz-label);
    color: var(--text-secondary);
    line-height: 1.5;
  }

  /* choice cards (theme / density). */
  .cards.three {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: var(--space-3);
  }

  /* the interface-size step: five compact choices in a row. */
  .cards.five {
    display: grid;
    grid-template-columns: repeat(6, 1fr);
    gap: var(--space-2);
    margin-bottom: var(--space-6);
  }

  .cards.five .card {
    padding: var(--space-4) var(--space-2);
  }

  .cards.four {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: var(--space-2);
    margin-bottom: var(--space-4);
  }
  .cards.four .card {
    padding: var(--space-4) var(--space-2);
  }

  /* the extras step: settings on the left, a live hover preview on the right. */
  .choose.wide {
    width: 100%;
  }
  .extras {
    display: grid;
    grid-template-columns: 1.15fr 0.85fr;
    gap: var(--space-5);
    margin: var(--space-4) 0 var(--space-2);
    align-items: start;
  }
  .extras-settings {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .ex-group {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-2);
    border-radius: var(--radius-card);
    border: var(--hairline) solid transparent;
    transition: border-color 0.12s ease, background 0.12s ease;
  }
  .ex-group:hover {
    border-color: var(--border-subtle);
    background: var(--surface-raised);
  }
  .ex-label {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }
  .badge-experimental {
    display: inline-block;
    margin-inline-start: var(--space-2);
    padding: 1px 6px;
    border-radius: var(--radius-control);
    background: var(--warning-bg, var(--surface-sunken));
    color: var(--warning, var(--text-secondary));
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    text-transform: uppercase;
    letter-spacing: 0.03em;
    vertical-align: middle;
  }
  .ex-toggle {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }
  .style-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: var(--space-2);
  }
  .style-card {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    cursor: var(--cursor-action);
  }
  .style-card.active {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent);
  }
  .style-img {
    width: 34px;
    height: 34px;
    border-radius: 999px;
  }
  .style-label {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .extras-preview {
    position: sticky;
    top: 0;
    min-height: 190px;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-sunken);
  }
  .pv-center {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-3);
    text-align: center;
  }
  .pv-avatar {
    width: 64px;
    height: 64px;
    border-radius: 999px;
  }
  .pv-cap {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
  .pv-sample {
    margin: 0;
    color: var(--text-primary);
    line-height: 1.5;
  }
  .pv-rows {
    width: 100%;
  }
  .pv-row {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3);
    background: var(--surface-raised);
    border-radius: var(--radius-control);
  }
  .pv-row.bar::before {
    content: '';
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    width: 3px;
    border-radius: 999px 0 0 999px;
    background: var(--warning);
  }
  .pv-from {
    flex: 1;
    font-size: var(--fz-label);
    color: var(--text-primary);
  }
  .pv-flag {
    color: var(--warning);
  }
  .pv-time {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .card {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-5) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    color: var(--text-primary);
    cursor: var(--cursor-action);
    transition: border-color 0.15s ease, transform 0.15s ease;
  }

  .card:hover {
    transform: translateY(-2px);
  }

  .card.active {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent) inset;
  }

  .card-icon {
    color: var(--text-secondary);
  }

  .card.active .card-icon {
    color: var(--accent);
  }

  .card-label {
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
  }

  .tick {
    position: absolute;
    top: var(--space-2);
    right: var(--space-2);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    border-radius: 999px;
    background: var(--accent);
    color: var(--accent-fg);
  }

  /* privacy-mode choice (two cards, each with a description of what it turns on/off). */
  .privacy-cards {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: var(--space-3);
  }

  .privacy-card {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-2);
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    color: var(--text-primary);
    text-align: start;
    cursor: var(--cursor-action);
    transition: border-color 0.15s ease, transform 0.15s ease;
  }

  .privacy-card:hover {
    transform: translateY(-2px);
  }

  .privacy-card.active {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent) inset;
  }

  .pc-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .pc-icon {
    color: var(--text-secondary);
  }

  .privacy-card.active .pc-icon {
    color: var(--accent);
  }

  .pc-label {
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
  }

  .pc-recommended {
    padding: 1px 6px;
    border-radius: var(--radius-control);
    background: var(--accent);
    color: var(--accent-fg);
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }

  .pc-body {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
    line-height: 1.5;
  }

  /* density illustration bars. */
  .bars {
    display: flex;
    flex-direction: column;
    width: 100%;
    padding: 0 var(--space-3);
  }

  .bar {
    height: 6px;
    border-radius: 999px;
    background: var(--border-strong, var(--text-tertiary));
    opacity: 0.5;
  }

  .bar.short {
    width: 60%;
  }

  /* accent swatches + live preview. */
  .swatches {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: var(--space-3);
    margin-bottom: var(--space-5);
  }

  .swatch {
    width: 40px;
    height: 40px;
    border-radius: 999px;
    border: 2px solid transparent;
    background: var(--sw);
    color: #fff;
    cursor: var(--cursor-action);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: transform 0.15s ease;
  }

  .swatch:hover {
    transform: scale(1.08);
  }

  .swatch.active {
    border-color: var(--text-primary);
  }

  /* the native color picker, shown as a round swatch tinted with the current
     accent and a palette glyph. the real input is an invisible overlay. */
  .custom-swatch {
    width: 40px;
    height: 40px;
    border-radius: 999px;
    border: var(--hairline) dashed var(--border-strong, var(--border-default));
    overflow: hidden;
    cursor: var(--cursor-action);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    position: relative;
    background: var(--accent);
    color: var(--accent-fg);
  }

  .custom-swatch.active {
    box-shadow: 0 0 0 2px var(--accent);
    border-style: solid;
  }

  .picker-wrap {
    display: flex;
    justify-content: center;
    margin-bottom: var(--space-6);
  }

  /* default-mail-client offer step. */
  .default-mail {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-5);
    margin-bottom: var(--space-6);
  }

  .dm-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 84px;
    height: 84px;
    border-radius: 999px;
    background: var(--surface-sunken);
    color: var(--accent);
    border: var(--hairline) solid var(--border-subtle);
  }

  .dm-cta {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-4) var(--space-6);
    border-radius: var(--radius-control);
    border: none;
    background: var(--accent);
    color: var(--accent-fg);
    font-size: var(--fz-body);
    font-weight: var(--fw-medium);
    cursor: var(--cursor-action);
  }

  .dm-cta:hover {
    filter: brightness(1.05);
  }

  .dm-cta:disabled {
    opacity: 0.6;
    cursor: default;
  }

  /* mailbox provider cards. */
  .providers {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    margin-bottom: var(--space-6);
  }

  .provider {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    width: 100%;
    padding: var(--space-4) var(--space-5);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    color: var(--text-primary);
    cursor: var(--cursor-action);
    transition: border-color 0.15s ease, transform 0.15s ease;
  }

  .provider:hover {
    transform: translateY(-2px);
    border-color: var(--accent);
  }

  .p-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    flex-shrink: 0;
    border-radius: var(--radius-card);
    background: var(--surface-sunken);
    color: var(--accent);
  }

  .p-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1;
    text-align: start;
  }

  .p-title {
    font-size: var(--fz-body);
    font-weight: var(--fw-semibold);
  }

  .p-sub {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  /* choose-step rhythm: give the heading, preview and controls room to breathe
     so the steps do not feel cramped. */
  .choose {
    padding: var(--space-4) 0;
  }

  .choose h2 {
    margin-bottom: var(--space-5);
  }

  .choose :global(.mock) {
    margin-bottom: var(--space-5);
  }

  .cards.three {
    margin-bottom: var(--space-6);
  }

  /* the liability step reads as prose, so it stays narrow and left-aligned
     instead of following the centered card layouts. */
  .liability {
    max-width: 62ch;
    text-align: start;
  }

  /* shared nav + buttons. */
  .nav {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: var(--space-3);
    margin-top: var(--space-6);
  }

  .primary,
  .ghost {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-4) var(--space-6);
    border-radius: var(--radius-control);
    font-size: var(--fz-body);
    font-weight: var(--fw-medium);
    cursor: var(--cursor-action);
    border: var(--hairline) solid var(--border-default);
  }

  .primary {
    background: var(--accent);
    color: var(--accent-fg);
    border-color: transparent;
  }

  .primary:hover {
    filter: brightness(1.05);
  }

  .primary:disabled {
    opacity: 0.5;
    filter: none;
    cursor: not-allowed;
  }

  .primary.big {
    padding: var(--space-5) var(--space-6);
    font-size: var(--fz-body);
  }

  .ghost {
    background: transparent;
    color: var(--text-secondary);
  }

  .ghost:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  /* progress dots. */
  .dots {
    position: absolute;
    bottom: var(--space-6);
    display: flex;
    gap: var(--space-2);
  }

  .dot {
    width: 7px;
    height: 7px;
    border-radius: 999px;
    background: var(--border-strong, var(--border-default));
    opacity: 0.5;
    transition: width 0.2s ease, opacity 0.2s ease, background 0.2s ease;
  }

  .dot.on {
    width: 20px;
    opacity: 1;
    background: var(--accent);
  }
</style>
