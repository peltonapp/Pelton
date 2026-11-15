<script lang="ts">
  // the Privacy page: what mail may load from the network, the allowlist of
  // trusted senders, and the proxy all of Pelton's connections go through.
  import { createEventDispatcher } from 'svelte'
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import NetworkSection from '../../settings/NetworkSection.svelte'
  import RemoteContentPreview from '../skeletons/RemoteContentPreview.svelte'
  import { prefs, setAlwaysLoadImages, setBlockTrackingPixels } from '../../../stores/prefs'
  import { t } from '../../../lib/i18n'
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
