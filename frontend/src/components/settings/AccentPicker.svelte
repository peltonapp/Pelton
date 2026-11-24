<script lang="ts">
  // the accent picker: preset swatches plus a free hex input with live
  // validation. only one color is ever chosen; selection background and link
  // derivations follow automatically from it via tokens.css. invalid hex is
  // rejected and never applied.
  import { IconCheck } from '@tabler/icons-svelte'
  import { prefs, setAccent } from '../../stores/prefs'
  import { accentPresets, accentTone, canonicalAccent, contrastForeground, darkScheme, isValidHex, normalizeHex } from '../../theme/accent'
  import { t } from '../../lib/i18n'

  // the hex text the user is typing, seeded from the current accent.
  let hexInput = ''
  let touched = false

  // seed the input from the stored accent until the user edits it.
  $: if (!touched) {
    hexInput = $prefs.accent
  }

  $: valid = isValidHex(hexInput)
  $: classic = $prefs.design === 'classic'
  $: presets = accentPresets($prefs.design)
  $: current = canonicalAccent($prefs.accent, $prefs.design)
  // the new design shows each swatch in the tone the live theme paints it in.
  $: shown = (hex: string): string => (classic ? hex : accentTone(hex, $prefs.design, $darkScheme))

  function pick(hex: string): void {
    touched = false
    setAccent(hex)
  }

  function onHexInput(event: Event): void {
    touched = true
    hexInput = (event.currentTarget as HTMLInputElement).value
    if (isValidHex(hexInput)) {
      setAccent(hexInput)
    }
  }
</script>

<div class="accent">
  <span class="label">{$t('settingsPanel.accentLabel')}</span>

  <div class="swatches" role="group" aria-label={$t('settingsPanel.accentPresets')}>
    {#each presets as preset (preset)}
      <button
        type="button"
        class="swatch"
        class:selected={normalizeHex(preset) === current}
        style={`background:${shown(preset)}`}
        aria-label={`${$t('settingsPanel.accentLabel')} ${preset}`}
        aria-pressed={normalizeHex(preset) === current}
        on:click={() => pick(preset)}
      >
        {#if normalizeHex(preset) === current}
          <IconCheck size={14} stroke={2.4} color={classic ? '#ffffff' : contrastForeground(shown(preset))} />
        {/if}
      </button>
    {/each}

    <span class="preview" style={`background:${shown(current)}`} aria-hidden="true"></span>
    <input
      class="hex"
      class:invalid={hexInput !== '' && !valid}
      type="text"
      spellcheck="false"
      autocomplete="off"
      aria-label={$t('settingsPanel.accentHexValue')}
      placeholder="#465AF2"
      value={hexInput}
      on:input={onHexInput}
    />
  </div>

  {#if hexInput !== '' && !valid}
    <p class="hint">{$t('settingsPanel.accentHexHint')}</p>
  {/if}
</div>

<style>
  .accent {
    padding: var(--space-3) 0;
  }

  .label {
    display: block;
    font-size: var(--fz-body);
    color: var(--text-primary);
    margin-bottom: var(--space-3);
  }

  .swatches {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }

  .swatch {
    width: 26px;
    height: 26px;
    border-radius: var(--radius-control);
    border: var(--hairline) solid var(--border-strong);
    cursor: var(--cursor-action);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0;
  }

  .swatch.selected {
    outline: 2px solid var(--text-primary);
    outline-offset: 1px;
  }

  .preview {
    width: 26px;
    height: 26px;
    border-radius: var(--radius-control);
    border: var(--hairline) solid var(--border-strong);
    margin-inline-start: var(--space-2);
  }

  .hex {
    width: 100px;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    padding: var(--space-2) var(--space-3);
    font-family: var(--font-mono);
    font-size: var(--fz-label);
  }

  .hex.invalid {
    border-color: var(--danger);
  }

  .hint {
    margin: var(--space-2) 0 0;
    font-size: var(--fz-meta);
    color: var(--danger);
  }
</style>
