<script lang="ts">
  // the keyboard step of the setup: the two keys worth knowing first, read
  // from the live bindings, and whether the letter keys move through lists
  // the vim way.
  import ChoiceCards from '../settings2/ui/ChoiceCards.svelte'
  import AppVimPreview from '../settings2/skeletons/AppVimPreview.svelte'
  import { bindings } from '../../stores/shortcuts'
  import { prefs, setAppVimMode } from '../../stores/prefs'
  import { shortcutLabel, t } from '../../lib/i18n'

  $: keys = [
    { action: 'command-palette', label: $t('setup.keys.palette') },
    { action: 'shortcut-sheet', label: $t('setup.keys.sheet') },
  ].map((k) => ({ ...k, combo: ($bindings as Record<string, string>)[k.action] ?? '' }))

  $: styles = [
    { key: 'standard', label: $t('setup.keys.standard'), hint: $t('setup.keys.standard.hint') },
    { key: 'vim', label: $t('setup.keys.vim'), hint: $t('setup.keys.vim.hint') },
  ]
</script>

<dl class="keys">
  {#each keys as k (k.action)}
    <div class="key">
      <dt>{k.label}</dt>
      <dd>
        {#if k.combo}
          <kbd>{shortcutLabel(k.combo)}</kbd>
        {:else}
          <span class="unbound">{$t('setup.keys.unbound')}</span>
        {/if}
      </dd>
    </div>
  {/each}
</dl>

<h3 class="label">{$t('setup.keys.style')}</h3>
<ChoiceCards
  label={$t('setup.keys.style')}
  value={$prefs.appVimMode ? 'vim' : 'standard'}
  options={styles}
  minWidth={200}
  on:change={(e) => setAppVimMode(e.detail === 'vim')}
  let:option
>
  <AppVimPreview enabled={option.key === 'vim'} />
</ChoiceCards>
<p class="later">{$t('setup.keys.later')}</p>

<style>
  .keys {
    margin: 0 0 var(--space-6);
    border-top: var(--hairline) solid var(--border-default);
  }

  .key {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    padding: var(--space-3) 0;
    border-bottom: var(--hairline) solid var(--border-default);
  }

  dt {
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  dd {
    margin: 0;
  }

  kbd {
    display: inline-flex;
    align-items: center;
    min-height: var(--chip-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-strong);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-primary);
  }

  .unbound,
  .later {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .later {
    margin: var(--space-4) 0 0;
  }

  .label {
    margin: 0 0 var(--space-3);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }
</style>
