<script lang="ts">
  // the Shortcuts page: hints and vim navigation for the keyboard, then the
  // editable list of key bindings.
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import ShortcutSettings from '../../settings/ShortcutSettings.svelte'
  import ShortcutHintPreview from '../skeletons/ShortcutHintPreview.svelte'
  import PalettePreview from '../skeletons/PalettePreview.svelte'
  import AppVimPreview from '../skeletons/AppVimPreview.svelte'
  import { prefs, setShortcutHints, setAppVimMode, setCoachHints, setCoachHold, setShowTips } from '../../../stores/prefs'
  import { paletteQuickSelect, setPaletteQuickSelect } from '../../../stores/palette'
  import { resetCoachHints } from '../../../stores/coach'
  import { resetTips } from '../../../stores/firsttips'
  import { isMac, modSymbol, t } from '../../../lib/i18n'
  import '../../../theme/settings2-sections-system.css'

  $: appVimLabel = `${$t('onboarding.extras.appVim')} (${$t('common.experimental')})`

  const modifierName = isMac ? `${modSymbol} Command` : modSymbol

  let resetDone = false

  function reset(): void {
    resetCoachHints()
    resetDone = true
  }

  let tipsResetDone = false

  function resetAllTips(): void {
    resetTips()
    tipsResetDone = true
  }
</script>

<Card title={$t('settings2.shortcuts.general')}>
  <Row id="shortcut-hints" label={$t('settingsPanel.toggle.shortcutHints')}>
    <ShortcutHintPreview slot="preview" hints={$prefs.showShortcutHints} />
    <ToggleSwitch
      checked={$prefs.showShortcutHints}
      label={$t('settingsPanel.toggle.shortcutHints')}
      on:change={(e) => setShortcutHints(e.detail)}
    />
  </Row>
  <Row id="coach-hints" label={$t('coach.settings.hints')} hint={$t('coach.settings.hintsHint')}>
    <ToggleSwitch checked={$prefs.coachHints} label={$t('coach.settings.hints')} on:change={(e) => setCoachHints(e.detail)} />
  </Row>
  <Row id="coach-hold" label={$t('coach.settings.hold')} hint={$t('coach.settings.holdHint').replace('{key}', modifierName)}>
    <ToggleSwitch checked={$prefs.coachHold} label={$t('coach.settings.hold')} on:change={(e) => setCoachHold(e.detail)} />
  </Row>
  <Row id="coach-reset" label={$t('coach.settings.reset')} hint={$t('coach.settings.resetHint')}>
    <button type="button" class="action" on:click={reset}>{resetDone ? $t('coach.settings.resetDone') : $t('coach.settings.resetButton')}</button>
  </Row>
  <Row id="tips" label={$t('tips.settings.show')} hint={$t('tips.settings.showHint')}>
    <ToggleSwitch checked={$prefs.showTips} label={$t('tips.settings.show')} on:change={(e) => setShowTips(e.detail)} />
  </Row>
  <Row id="tips-reset" label={$t('tips.settings.reset')} hint={$t('tips.settings.resetHint')}>
    <button type="button" class="action" on:click={resetAllTips}>{tipsResetDone ? $t('coach.settings.resetDone') : $t('tips.settings.resetButton')}</button>
  </Row>
  <Row id="palette-quick-select" label={$t('settingsPanel.toggle.paletteQuickSelect')} hint={$t('settingsPanel.hint.paletteQuickSelect')}>
    <PalettePreview slot="preview" quickSelect={$paletteQuickSelect} />
    <ToggleSwitch
      checked={$paletteQuickSelect}
      label={$t('settingsPanel.toggle.paletteQuickSelect')}
      on:change={(e) => setPaletteQuickSelect(e.detail)}
    />
  </Row>
  <Row id="app-vim" label={appVimLabel} hint={`${$t('settingsPanel.hint.appVim')} ${$t('settingsPanel.hint.appVimDetail')}`}>
    <AppVimPreview slot="preview" enabled={$prefs.appVimMode} />
    <ToggleSwitch checked={$prefs.appVimMode} label={$t('onboarding.extras.appVim')} on:change={(e) => setAppVimMode(e.detail)} />
  </Row>
</Card>

<Card title={$t('settings.shortcuts')} bare>
  <div class="s2-section" data-setting="bindings">
    <ShortcutSettings />
  </div>
</Card>

<style>
  .action {
    padding: var(--space-2) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-family: inherit;
    font-size: var(--fz-label);
    color: var(--text-primary);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .action:hover {
    background: var(--surface-hover);
  }
</style>
