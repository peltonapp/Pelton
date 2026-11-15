<script lang="ts">
  // the Composing page: which editor a new message starts in, how recipients
  // are entered, signature placement, vim keys, and the undo-send window.
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import ChoiceCards from '../ui/ChoiceCards.svelte'
  import ComposeSkeleton from '../skeletons/ComposeSkeleton.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import Select from '../../common/Select.svelte'
  import VimPreview from '../skeletons/VimPreview.svelte'
  import MarkdownPreview from '../skeletons/MarkdownPreview.svelte'
  import CaretPreview from '../skeletons/CaretPreview.svelte'
  import AutocompletePreview from '../skeletons/AutocompletePreview.svelte'
  import RecipientsPreview from '../skeletons/RecipientsPreview.svelte'
  import SignaturePreview from '../skeletons/SignaturePreview.svelte'
  import SendDelayPreview from '../skeletons/SendDelayPreview.svelte'
  import {
    prefs,
    setComposeOpenIn,
    setComposeAutocomplete,
    setComposeChips,
    setComposeMarkdown,
    setSmoothCaret,
    setSignatureAboveQuote,
    setComposeVimMode,
    setSendDelay,
    setWarnMissingAttachment,
    setWarnEmptySubject,
  } from '../../../stores/prefs'
  import SendCheckSkeleton from '../skeletons/SendCheckSkeleton.svelte'
  import { t } from '../../../lib/i18n'
  import type { ComposeOpenIn } from '../../../lib/types'

  $: openInOptions = [
    { key: 'window', label: $t('composing.openIn.window'), hint: $t('composing.openIn.window.hint') },
    { key: 'docked', label: $t('composing.openIn.docked'), hint: $t('composing.openIn.docked.hint') },
    { key: 'tab', label: $t('composing.openIn.tab'), hint: $t('composing.openIn.tab.hint') },
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
  <Row id="compose-open-in" label={$t('composing.openIn')} hint={$t('composing.openIn.hint')} stacked>
    <ChoiceCards
      label={$t('composing.openIn')}
      value={$prefs.composeOpenIn}
      options={openInOptions}
      minWidth={170}
      on:change={(e) => setComposeOpenIn(e.detail as ComposeOpenIn)}
      let:option
    >
      <ComposeSkeleton mode={option.key as ComposeOpenIn} />
    </ChoiceCards>
  </Row>
  <Row id="compose-markdown" label={$t('composing.markdown')} hint={$t('composing.markdown.hint')}>
    <MarkdownPreview slot="preview" on={$prefs.composeMarkdown} />
    <ToggleSwitch checked={$prefs.composeMarkdown} label={$t('composing.markdown')} on:change={(e) => setComposeMarkdown(e.detail)} />
  </Row>
  <Row id="smooth-caret" label={$t('composing.smoothCaret')} hint={$t('composing.smoothCaret.hint')}>
    <CaretPreview slot="preview" smooth={$prefs.smoothCaret} />
    <ToggleSwitch checked={$prefs.smoothCaret} label={$t('composing.smoothCaret')} on:change={(e) => setSmoothCaret(e.detail)} />
  </Row>
  <Row id="autocomplete" label={$t('settingsPanel.toggle.autocomplete')} hint={$t('settingsPanel.hint.autocomplete')}>
    <AutocompletePreview slot="preview" enabled={$prefs.composeAutocomplete} />
    <ToggleSwitch
      checked={$prefs.composeAutocomplete}
      label={$t('settingsPanel.toggle.autocomplete')}
      on:change={(e) => setComposeAutocomplete(e.detail)}
    />
  </Row>
  <Row id="chips" label={$t('settingsPanel.toggle.chipRecipients')} hint={chipsHint}>
    <RecipientsPreview slot="preview" chips={$prefs.composeChips} />
    <ToggleSwitch
      checked={$prefs.composeChips}
      label={$t('settingsPanel.toggle.chipRecipients')}
      on:change={(e) => setComposeChips(e.detail)}
    />
  </Row>
  <Row id="signature-above-quote" label={$t('settingsPanel.toggle.signatureAboveQuote')}>
    <SignaturePreview slot="preview" above={$prefs.signatureAboveQuote} />
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
    <SendDelayPreview slot="preview" seconds={$prefs.sendDelaySeconds} />
    <Select
      value={String($prefs.sendDelaySeconds)}
      ariaLabel={$t('settingsPanel.label.undoSendWindow')}
      items={sendDelays}
      on:change={(e) => setSendDelay(Number(e.detail))}
    />
  </Row>
  <Row id="warn-missing-attachment" label={$t('sendCheck.setting.attachment')} hint={$t('sendCheck.setting.attachmentHint')}>
    <SendCheckSkeleton slot="preview" issue="missingAttachment" />
    <ToggleSwitch
      checked={$prefs.warnMissingAttachment}
      label={$t('sendCheck.setting.attachment')}
      on:change={(e) => setWarnMissingAttachment(e.detail)}
    />
  </Row>
  <Row id="warn-empty-subject" label={$t('sendCheck.setting.subject')} hint={$t('sendCheck.setting.subjectHint')}>
    <SendCheckSkeleton slot="preview" issue="emptySubject" />
    <ToggleSwitch
      checked={$prefs.warnEmptySubject}
      label={$t('sendCheck.setting.subject')}
      on:change={(e) => setWarnEmptySubject(e.detail)}
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
