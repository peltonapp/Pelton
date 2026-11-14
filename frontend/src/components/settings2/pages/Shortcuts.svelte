<script lang="ts">
  // the Shortcuts page: hints and vim navigation for the keyboard, then the
  // editable list of key bindings.
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import ShortcutSettings from '../../settings/ShortcutSettings.svelte'
  import { prefs, setShortcutHints, setAppVimMode } from '../../../stores/prefs'
  import { paletteQuickSelect, setPaletteQuickSelect } from '../../../stores/palette'
  import { t } from '../../../lib/i18n'
  import '../../../theme/settings2-sections-system.css'

  $: appVimLabel = `${$t('onboarding.extras.appVim')} (${$t('common.experimental')})`
</script>

<Card title={$t('settings2.shortcuts.general')}>
  <Row id="shortcut-hints" label={$t('settingsPanel.toggle.shortcutHints')}>
    <ToggleSwitch
      checked={$prefs.showShortcutHints}
      label={$t('settingsPanel.toggle.shortcutHints')}
      on:change={(e) => setShortcutHints(e.detail)}
    />
  </Row>
  <Row id="palette-quick-select" label={$t('settingsPanel.toggle.paletteQuickSelect')} hint={$t('settingsPanel.hint.paletteQuickSelect')}>
    <ToggleSwitch
      checked={$paletteQuickSelect}
      label={$t('settingsPanel.toggle.paletteQuickSelect')}
      on:change={(e) => setPaletteQuickSelect(e.detail)}
    />
  </Row>
  <Row id="app-vim" label={appVimLabel} hint={`${$t('settingsPanel.hint.appVim')} ${$t('settingsPanel.hint.appVimDetail')}`}>
    <ToggleSwitch checked={$prefs.appVimMode} label={$t('onboarding.extras.appVim')} on:change={(e) => setAppVimMode(e.detail)} />
  </Row>
</Card>

<Card title={$t('settings.shortcuts')} bare>
  <div class="s2-section" data-setting="bindings">
    <ShortcutSettings />
  </div>
</Card>
