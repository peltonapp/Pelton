<script lang="ts">
  // the Privacy page: what mail may load from the network, the allowlist of
  // trusted senders, link cleaning, the app lock, and the proxy all of
  // Pelton's connections
  // go through.
  import { createEventDispatcher } from 'svelte'
  import Card from '../ui/Card.svelte'
  import AppLockCard from '../AppLockCard.svelte'
  import Row from '../ui/Row.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import NetworkSection from '../../settings/NetworkSection.svelte'
  import RemoteContentPreview from '../skeletons/RemoteContentPreview.svelte'
  import Segmented from '../ui/Segmented.svelte'
  import {
    prefs,
    setAlwaysLoadImages,
    setAutoconfigLookup,
    setBlockTrackingPixels,
    setCleanLinks,
    setPrivacyCounters,
    setUnsubscribeGraceDays,
  } from '../../../stores/prefs'
  import { openMailScreen } from '../../../stores/mailscreen'
  import { t } from '../../../lib/i18n'
  import type { AutoconfigLookup } from '../../../lib/types'

  $: lookupOptions = [
    { key: 'ask', label: $t('privacy.lookup.ask') },
    { key: 'allow', label: $t('privacy.lookup.allow') },
    { key: 'manual', label: $t('privacy.lookup.manual') },
  ]
  import '../../../theme/settings2-sections-system.css'

  const dispatch = createEventDispatcher<{ close: void }>()

  // enabling the global remote-image override is guarded by a tracking warning.
  let confirmImages = false
  function onImagesToggle(checked: boolean): void {
    if (checked && !$prefs.alwaysLoadImages) {
      confirmImages = true
    } else {
      setAlwaysLoadImages(false)
    }
  }
  function confirmEnableImages(): void {
    setAlwaysLoadImages(true)
    confirmImages = false
  }

  let allowlistOpen = false

  function onGrace(value: string): void {
    const days = Math.round(Number(value))
    if (Number.isFinite(days) && days >= 1 && days <= 90) {
      setUnsubscribeGraceDays(days)
    }
  }

  function openReport(): void {
    openMailScreen('privacy')
    dispatch('close')
  }
</script>

<Card title={$t('settings2.privacy.remote')}>
  <Row id="always-load-images" label={$t('settingsPanel.toggle.alwaysLoadImages')} hint={$t('settingsPanel.hint.remoteImagesToggle')}>
    <RemoteContentPreview slot="preview" images={$prefs.alwaysLoadImages} blockPixels={$prefs.blockTrackingPixels} />
    <ToggleSwitch
      checked={$prefs.alwaysLoadImages}
      label={$t('settingsPanel.toggle.alwaysLoadImages')}
      on:change={(e) => onImagesToggle(e.detail)}
    />
  </Row>
  {#if confirmImages}
    <div class="s2-warn">
      <p>{$t('settingsPanel.warn.remoteImages')}</p>
      <div class="actions">
        <button type="button" class="s2-btn" on:click={() => (confirmImages = false)}>{$t('settingsPanel.button.cancel')}</button>
        <button type="button" class="s2-btn danger" on:click={confirmEnableImages}>{$t('settingsPanel.button.enableAnyway')}</button>
      </div>
    </div>
  {/if}
  <Row id="block-tracking-pixels" label={$t('settingsPanel.toggle.blockTrackingPixels')} hint={$t('settingsPanel.hint.blockTrackingPixelsDetail')}>
    <RemoteContentPreview slot="preview" images blockPixels={$prefs.blockTrackingPixels} />
    <ToggleSwitch
      checked={$prefs.blockTrackingPixels}
      label={$t('settingsPanel.toggle.blockTrackingPixels')}
      on:change={(e) => setBlockTrackingPixels(e.detail)}
    />
  </Row>
  <Row id="allowlist" label={$t('settingsPanel.label.manageWhitelist')} hint={$t('settingsPanel.hint.manageWhitelist')}>
    <button type="button" class="s2-btn" on:click={() => (allowlistOpen = true)}>
      {$t('settingsPanel.button.manageWhitelist')}
    </button>
  </Row>
</Card>

<Card title={$t('settings2.privacy.links')}>
  <Row id="clean-links" label={$t('links.clean.label')} hint={$t('links.clean.hint')}>
    <ToggleSwitch checked={$prefs.cleanLinks} label={$t('links.clean.label')} on:change={(e) => setCleanLinks(e.detail)} />
  </Row>
  <Row id="autoconfig-lookup" label={$t('privacy.lookup.label')} hint={$t('privacy.lookup.hint')}>
    <Segmented
      label={$t('privacy.lookup.label')}
      value={$prefs.autoconfigLookup}
      options={lookupOptions}
      on:change={(e) => void setAutoconfigLookup(e.detail as AutoconfigLookup)}
    />
  </Row>
</Card>
<Card title={$t('privacy.report.title')}>
  <Row id="privacy-counters" label={$t('privacy.counters.label')} hint={$t('privacy.counters.hint')}>
    <ToggleSwitch checked={$prefs.privacyCounters} label={$t('privacy.counters.label')} on:change={(e) => setPrivacyCounters(e.detail)} />
  </Row>
  <Row id="unsubscribe-grace" label={$t('privacy.grace.label')} hint={$t('privacy.grace.hint')}>
    <input
      class="cal-field grace"
      type="number"
      min="1"
      max="90"
      aria-label={$t('privacy.grace.label')}
      value={$prefs.unsubscribeGraceDays}
      on:change={(e) => onGrace(e.currentTarget.value)}
    />
  </Row>
  <Row id="privacy-report" label={$t('privacy.report.title')}>
    <button type="button" class="s2-btn" on:click={openReport}>{$t('privacy.report.title')}</button>
  </Row>
</Card>
<AppLockCard />

<Card bare>
  <div class="s2-section" data-setting="proxy">
    <NetworkSection />
  </div>
</Card>

{#if allowlistOpen}
  {#await import('../../settings/ImageAllowlistModal.svelte') then m}
    <svelte:component
      this={m.default}
      on:close={() => (allowlistOpen = false)}
      on:openMessage={() => {
        allowlistOpen = false
        dispatch('close')
      }}
    />
  {/await}
{/if}

<style>
  .grace {
    width: 6em;
  }
</style>
