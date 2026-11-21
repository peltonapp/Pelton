<script lang="ts">
  // step 1 of the add-mailbox wizard: pick a provider. each tile seeds the auth
  // method and servers; "other" uses autodiscovery.
  import { createEventDispatcher } from 'svelte'
  import { IconBrandGoogle, IconBrandWindows, IconBrandApple, IconBrandYahoo, IconMail, IconServer } from '@tabler/icons-svelte'
  import { providerPresets, type ProviderPreset } from '../../lib/providers'
  import { t } from '../../lib/i18n'

  const dispatch = createEventDispatcher<{ pick: ProviderPreset }>()

  // an icon per preset id, falling back to a generic mail glyph.
  const icons: Record<string, typeof IconMail> = {
    gmail: IconBrandGoogle,
    outlook: IconBrandWindows,
    icloud: IconBrandApple,
    yahoo: IconBrandYahoo,
    fastmail: IconMail,
    custom: IconServer,
  }
</script>

<div class="step">
  <h3>{$t('wizard.step.provider.title')}</h3>
  <p class="lead">{$t('wizard.step.provider.lead')}</p>

  <div class="grid">
    {#each providerPresets as preset (preset.id)}
      <button type="button" class="tile" on:click={() => dispatch('pick', preset)}>
        <svelte:component this={icons[preset.id] ?? IconMail} size={22} stroke={1.5} />
        <span class="label">{preset.labelKey ? $t(preset.labelKey) : preset.label}</span>
        {#if preset.kind === 'oauth'}
          <span class="tag">{$t('wizard.provider.signIn')}</span>
        {/if}
      </button>
    {/each}
  </div>
</div>

<style>
  .step {
    display: flex;
    flex-direction: column;
  }

  h3 {
    margin: 0;
    font-size: var(--fz-title);
    font-weight: var(--fw-semibold);
  }

  .lead {
    margin: var(--space-2) 0 var(--space-5);
    color: var(--text-secondary);
    font-size: var(--fz-body);
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: var(--space-3);
  }

  .tile {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    color: var(--text-primary);
    cursor: var(--cursor-action);
    text-align: start;
  }

  .tile:hover {
    background: var(--surface-hover);
    border-color: var(--border-strong);
  }

  .label {
    flex: 1;
    font-size: var(--fz-body);
    font-weight: var(--fw-medium);
  }

  .tag {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    padding: 1px var(--space-2);
  }
</style>
