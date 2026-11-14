<script lang="ts">
  // the Composing page: which editor a new message starts in, how recipients
  // are entered, signature placement, vim keys, and the undo-send window.
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import Segmented from '../ui/Segmented.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import Select from '../../common/Select.svelte'
  import VimPreview from '../../onboarding/VimPreview.svelte'
  import {
    prefs,
    setDefaultEditorMode,
    setComposeAutocomplete,
    setComposeChips,
    setComposeMarkdown,
    setSignatureAboveQuote,
    setComposeVimMode,
    setSendDelay,
  } from '../../../stores/prefs'
  import { t } from '../../../lib/i18n'
  import type { EditorMode } from '../../../lib/types'

  $: editorModes = [
    { key: 'plaintext', label: $t('settingsPanel.editorMode.plaintext') },
    { key: 'markdown', label: $t('settingsPanel.editorMode.markdown') },
    { key: 'wysiwyg', label: $t('settingsPanel.editorMode.wysiwyg') },
  ]

  $: sendDelays = [
    { value: '0', label: $t('settingsPanel.unit.off') },
    { value: '5', label: $t('settingsPanel.unit.s5') },
    { value: '10', label: $t('settingsPanel.unit.s10') },
    { value: '30', label: $t('settingsPanel.unit.s30') },
    { value: '60', label: $t('settingsPanel.unit.s60') },
  ]

  $: chipsHint = $prefs.composeChips
    ? $t('settingsPanel.hint.chipRecipients')
    : `${$t('settingsPanel.hint.chipRecipients')} ${$t('settingsPanel.hint.plainRecipients')}`
</script>

<Card title={$t('settingsPanel.category.composingSending')}>
  <Row id="default-editor" label={$t('settingsPanel.label.defaultEditor')} hint={$t('settingsPanel.hint.defaultEditor')}>
    <Segmented
      label={$t('settingsPanel.label.defaultEditor')}
      value={$prefs.defaultEditorMode}
      options={editorModes}
      on:change={(e) => setDefaultEditorMode(e.detail as EditorMode)}
    />
  </Row>
  <Row id="compose-markdown" label={$t('composing.markdown')} hint={$t('composing.markdown.hint')}>
    <ToggleSwitch checked={$prefs.composeMarkdown} label={$t('composing.markdown')} on:change={(e) => setComposeMarkdown(e.detail)} />
  </Row>
  <Row id="autocomplete" label={$t('settingsPanel.toggle.autocomplete')} hint={$t('settingsPanel.hint.autocomplete')}>
    <ToggleSwitch
      checked={$prefs.composeAutocomplete}
      label={$t('settingsPanel.toggle.autocomplete')}
      on:change={(e) => setComposeAutocomplete(e.detail)}
    />
  </Row>
  <Row id="chips" label={$t('settingsPanel.toggle.chipRecipients')} hint={chipsHint}>
    <ToggleSwitch
      checked={$prefs.composeChips}
      label={$t('settingsPanel.toggle.chipRecipients')}
      on:change={(e) => setComposeChips(e.detail)}
    />
  </Row>
  <Row id="signature-above-quote" label={$t('settingsPanel.toggle.signatureAboveQuote')}>
    <ToggleSwitch
      checked={$prefs.signatureAboveQuote}
      label={$t('settingsPanel.toggle.signatureAboveQuote')}
      on:change={(e) => setSignatureAboveQuote(e.detail)}
    />
  </Row>
  <Row
    id="vim-editor"
    label={$t('onboarding.extras.vimEditor')}
    hint={`${$t('settingsPanel.hint.vimEditor')} ${$t('settingsPanel.hint.vimEditorDetail')}`}
  >
    <svelte:fragment slot="preview">
      <VimPreview enabled={true} />
    </svelte:fragment>
    <span class="badge">{$t('common.experimental')}</span>
    <ToggleSwitch
      checked={$prefs.composeVimMode}
      label={$t('onboarding.extras.vimEditor')}
      on:change={(e) => setComposeVimMode(e.detail)}
    />
  </Row>
</Card>

<Card title={$t('settingsPanel.category.sending')}>
  <Row id="undo-send" label={$t('settingsPanel.label.undoSendWindow')} hint={$t('settingsPanel.hint.undoSend')}>
    <Select
      value={String($prefs.sendDelaySeconds)}
      ariaLabel={$t('settingsPanel.label.undoSendWindow')}
      items={sendDelays}
      on:change={(e) => setSendDelay(Number(e.detail))}
    />
  </Row>
</Card>

<style>
  .badge {
    padding: 1px var(--space-2);
    border-radius: var(--radius-control);
    background: var(--warning-bg);
    color: var(--warning);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }
</style>
