import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'composing', id: 'compose-open-in', labelKey: 'composing.openIn', keywords: 'compose window docked panel tab new message reply open' },
  { page: 'composing', id: 'compose-markdown', labelKey: 'composing.markdown', keywords: 'markdown format typing live shortcuts' },
  { page: 'composing', id: 'autocomplete', labelKey: 'settingsPanel.toggle.autocomplete', keywords: 'autocomplete recipients suggestions address book' },
  { page: 'composing', id: 'chips', labelKey: 'settingsPanel.toggle.chipRecipients', keywords: 'chips recipients tokens comma' },
  { page: 'composing', id: 'signature-above-quote', labelKey: 'settingsPanel.toggle.signatureAboveQuote', keywords: 'signature footer reply quote above below' },
  { page: 'composing', id: 'vim-editor', labelKey: 'onboarding.extras.vimEditor', keywords: 'vim modal keybindings insert normal' },
  { page: 'composing', id: 'undo-send', labelKey: 'settingsPanel.label.undoSendWindow', keywords: 'undo send delay hold outgoing' },
]

export default entries
