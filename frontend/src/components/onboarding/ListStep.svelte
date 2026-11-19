<script lang="ts">
  // the message list step of onboarding: where sender pictures go and how they
  // are drawn, how much a row shows, and whether Pelton may load anything from
  // outside to show it.
  import Ph from '../common/Ph.svelte'
  import ChoiceCards from '../settings2/ui/ChoiceCards.svelte'
  import RowSkeleton from '../settings2/skeletons/RowSkeleton.svelte'
  import RowLayoutPreview from '../settings/RowLayoutPreview.svelte'
  import Segments from './Segments.svelte'
  import shieldIcon from '@phosphor-icons/core/assets/duotone/shield-check-duotone.svg?raw'
  import globeIcon from '@phosphor-icons/core/assets/duotone/globe-duotone.svg?raw'
  import {
    prefs,
    setAvatarPlacement,
    setAvatarStyle,
    setRowTemplate,
    setAvatarSource,
    setAlwaysLoadImages,
    setBlockTrackingPixels,
  } from '../../stores/prefs'
  import { pfpDataUri, type PfpStyle } from '../../lib/pfp'
  import { initials } from '../../lib/format'
  import { t } from '../../lib/i18n'
  import type { AvatarPlacement } from '../../lib/types'

  $: placements = [
    { key: 'column', label: $t('avatarPlacement.column'), hint: $t('avatarPlacement.column.hint') },
    { key: 'inline', label: $t('avatarPlacement.inline'), hint: $t('avatarPlacement.inline.hint') },
    { key: 'hidden', label: $t('avatarPlacement.hidden'), hint: $t('avatarPlacement.hidden.hint') },
  ]
  $: templates = [
    { key: 'relaxed', label: $t('onboarding.row.relaxed') },
    { key: 'comfortable', label: $t('onboarding.row.comfortable') },
    { key: 'compact', label: $t('onboarding.row.compact') },
    { key: 'single', label: $t('onboarding.row.single') },
  ]
  $: styles = [
    { key: 'initials' as PfpStyle, label: $t('onboarding.avatar.classic') },
    { key: 'mono' as PfpStyle, label: $t('onboarding.avatar.mono') },
    { key: 'pixel' as PfpStyle, label: $t('onboarding.avatar.pixel') },
    { key: 'geometric' as PfpStyle, label: $t('onboarding.avatar.geometric') },
  ]
  $: privacyModes = [
    { key: 'private', label: `${$t('onboarding.privacy.private.label')} (${$t('onboarding.privacy.recommended')})`, hint: $t('onboarding.privacy.private.body') },
    { key: 'normal', label: $t('onboarding.privacy.normal.label'), hint: $t('onboarding.privacy.normal.body') },
  ]

  const sampleEmail = 'potato@pelton.email'
  const sampleInitials = initials('', sampleEmail)

  // the privacy mode is read off the three settings it sets, so it always shows
  // what is in force, also when onboarding is run again.
  $: privacy = $prefs.avatarSource === 'pfp' && !$prefs.alwaysLoadImages && $prefs.blockTrackingPixels ? 'private' : 'normal'

  function setPrivacy(mode: string): void {
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
</script>

<section class="group">
  <h3 class="label">{$t('onboarding.v2.avatar.label')}</h3>
  <ChoiceCards
    label={$t('onboarding.v2.avatar.label')}
    value={$prefs.avatarPlacement}
    options={placements}
    minWidth={120}
    on:change={(e) => setAvatarPlacement(e.detail as AvatarPlacement)}
    let:option
  >
    <RowSkeleton avatar={option.key as AvatarPlacement} height={56} />
  </ChoiceCards>

  {#if $prefs.avatarPlacement !== 'hidden'}
    <div class="styles" role="radiogroup" aria-label={$t('onboarding.extras.avatarStyle')}>
      {#each styles as style (style.key)}
        <button
          type="button"
          role="radio"
          class="style"
          class:on={$prefs.avatarStyle === style.key}
          aria-checked={$prefs.avatarStyle === style.key}
          on:click={() => setAvatarStyle(style.key)}
        >
          <img src={pfpDataUri(style.key, sampleEmail, sampleInitials)} alt="" />
          <span>{style.label}</span>
        </button>
      {/each}
    </div>
  {/if}
</section>

<section class="group">
  <h3 class="label">{$t('settingsPanel.label.rowLayout')}</h3>
  <Segments label={$t('settingsPanel.label.rowLayout')} value={$prefs.rowTemplate} options={templates} on:change={(e) => setRowTemplate(e.detail)} />
  <div class="rows">
    <RowLayoutPreview />
  </div>
</section>

<section class="group">
  <h3 class="label">{$t('onboarding.privacyTitle')}</h3>
  <ChoiceCards label={$t('onboarding.privacyTitle')} value={privacy} options={privacyModes} minWidth={180} on:change={(e) => setPrivacy(e.detail)} let:option>
    <span class="art"><Ph svg={option.key === 'private' ? shieldIcon : globeIcon} size={20} /></span>
  </ChoiceCards>
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

  .styles {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .style {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-height);
    padding-block: var(--space-1);
    padding-inline: var(--space-1) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-size: var(--fz-label);
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    transition:
      border-color var(--motion-fast) var(--ease-out),
      box-shadow var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .style:active {
    transform: scale(0.96);
  }

  .style.on {
    border-color: var(--accent);
    box-shadow: 0 0 0 var(--hairline) var(--accent);
    color: var(--text-primary);
  }

  .style img {
    width: calc(var(--control-height) - var(--space-3));
    height: calc(var(--control-height) - var(--space-3));
    border-radius: 50%;
  }

  .rows {
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    overflow: hidden;
  }

  .art {
    display: flex;
    align-items: center;
    padding: var(--space-3);
    background: var(--surface-sunken);
    color: var(--text-secondary);
  }
</style>
