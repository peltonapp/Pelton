<script lang="ts">
  // text recognition settings for the pdf viewer: the engine (on macOS, Vision
  // or tesseract) and the tesseract languages, bundled or added from disk.
  // added languages are files the user brings; the app never downloads them.
  import { onMount } from 'svelte'
  import { IconPlus, IconFolderOpen, IconX } from '@tabler/icons-svelte'
  import SegmentedSetting from './SegmentedSetting.svelte'
  import { prefs, setOCREngine, setOCRLanguages } from '../../stores/prefs'
  import { t, locale, localeNames, type Locale } from '../../lib/i18n'
  import { addOCRLanguage, listOCRLanguages, openOCRFolder, removeOCRLanguage } from '../../lib/api'
  import { openURL } from '../../lib/browser'
  import { bundledOCRCodes, bundledOCRLanguages } from '../../lib/ocrlanguages'
  import { hasNativeOCR, ocrLanguageList, stopTesseract } from '../../lib/ocr'
  import { errorMessage, toastError } from '../../stores/toast'

  const tessdataURL = 'https://github.com/tesseract-ocr/tessdata'

  let native = false
  let userLangs: string[] = []

  // a bundled language is named the way the ui names that language.
  const names: Record<string, string> = Object.fromEntries(
    Object.entries(bundledOCRLanguages).map(([loc, code]) => [code, localeNames[loc as Locale]]),
  )

  onMount(async () => {
    native = await hasNativeOCR()
    await refresh()
  })

  async function refresh(): Promise<void> {
    try {
      userLangs = (await listOCRLanguages()).filter((c) => !bundledOCRCodes.includes(c))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  $: selected = ocrLanguageList($prefs.ocrLanguages, $locale)

  function choose(next: string[]): void {
    setOCRLanguages(next.join('+'))
    stopTesseract()
  }

  function toggle(code: string): void {
    if (selected.includes(code)) {
      if (selected.length > 1) {
        choose(selected.filter((c) => c !== code))
      }
    } else {
      choose([...selected, code])
    }
  }

  async function add(): Promise<void> {
    try {
      const code = await addOCRLanguage()
      if (!code) {
        return
      }
      await refresh()
      if (!selected.includes(code)) {
        choose([...selected, code])
      } else {
        stopTesseract()
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function remove(code: string): Promise<void> {
    try {
      await removeOCRLanguage(code)
      if (selected.includes(code)) {
        choose(selected.filter((c) => c !== code))
      }
      await refresh()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function openFolder(): Promise<void> {
    try {
      await openOCRFolder()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<p class="hint">{$t('settingsPanel.ocr.hint')}</p>

{#if native}
  <SegmentedSetting
    label={$t('settingsPanel.ocr.engine')}
    value={$prefs.ocrEngine === 'tesseract' ? 'tesseract' : 'native'}
    options={[
      { key: 'native', label: 'Vision' },
      { key: 'tesseract', label: 'Tesseract' },
    ]}
    on:change={(e) => setOCREngine(e.detail)}
  />
  <p class="hint">{$t('settingsPanel.ocr.engineHint')}</p>
{/if}

{#if !native || $prefs.ocrEngine === 'tesseract'}
  <div class="label">{$t('settingsPanel.ocr.languages')}</div>
  <p class="hint">{$t('settingsPanel.ocr.languagesHint')}</p>
  <div class="chips" role="group" aria-label={$t('settingsPanel.ocr.languages')}>
    {#each bundledOCRCodes as code (code)}
      <button type="button" class="chip" class:on={selected.includes(code)} aria-pressed={selected.includes(code)} on:click={() => toggle(code)}>
        {names[code] ?? code}
      </button>
    {/each}
    {#each userLangs as code (code)}
      <span class="chip user" class:on={selected.includes(code)}>
        <button type="button" class="chip-main" aria-pressed={selected.includes(code)} on:click={() => toggle(code)}>{code}</button>
        <button type="button" class="chip-x" title={$t('settingsPanel.ocr.remove')} aria-label={`${$t('settingsPanel.ocr.remove')} ${code}`} on:click={() => remove(code)}>
          <IconX size={12} stroke={1.8} />
        </button>
      </span>
    {/each}
  </div>
  <p class="hint">
    {$t('settingsPanel.ocr.addHint')}
    <button type="button" class="link" on:click={() => openURL(tessdataURL)}>{$t('settingsPanel.ocr.addLink')}</button>
  </p>
  <div class="tools">
    <button type="button" class="tool-btn" on:click={add}>
      <IconPlus size={15} stroke={1.6} />
      {$t('settingsPanel.ocr.add')}
    </button>
    <button type="button" class="tool-btn" on:click={openFolder}>
      <IconFolderOpen size={15} stroke={1.6} />
      {$t('language.openFolder')}
    </button>
  </div>
{/if}

<style>
  .hint {
    margin: 2px 0 var(--space-4);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    line-height: 1.5;
  }

  .label {
    font-size: var(--fz-body);
    color: var(--text-primary);
    margin-top: var(--space-3);
  }

  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin-bottom: var(--space-3);
  }

  .chip,
  .chip-main {
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    padding: var(--space-1) var(--space-3);
    cursor: var(--cursor-action);
  }
  .chip:hover,
  .chip-main:hover {
    background: var(--surface-hover);
  }
  .chip.on,
  .chip.on .chip-main {
    border-color: var(--accent);
    color: var(--text-primary);
    background: color-mix(in srgb, var(--accent) 14%, var(--surface-raised));
  }

  .chip.user {
    display: inline-flex;
    padding: 0;
    border: none;
    background: none;
  }
  .chip.user .chip-main {
    border-top-right-radius: 0;
    border-bottom-right-radius: 0;
    font-family: var(--font-mono);
  }
  .chip-x {
    display: inline-flex;
    align-items: center;
    border: var(--hairline) solid var(--border-default);
    border-left: none;
    border-radius: 0 var(--radius-control) var(--radius-control) 0;
    background: var(--surface-raised);
    color: var(--text-tertiary);
    padding: 0 var(--space-2);
    cursor: var(--cursor-action);
  }
  .chip-x:hover {
    color: var(--text-primary);
    background: var(--surface-hover);
  }

  .link {
    border: none;
    background: transparent;
    color: var(--accent);
    cursor: var(--cursor-action);
    font: inherit;
    padding: 0;
    text-decoration: underline;
  }

  .tools {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .tool-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }
  .tool-btn:hover {
    background: var(--surface-hover);
  }
</style>
