import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'composing', id: 'compose-open-in', labelKey: 'composing.openIn', keywords: 'compose window docked panel tab new message reply open' },
  { page: 'composing', id: 'compose-markdown', labelKey: 'composing.markdown', keywords: 'markdown format typing live shortcuts' },
  { page: 'composing', id: 'smooth-caret', labelKey: 'composing.smoothCaret', keywords: 'smooth typing caret cursor animation glide word' },
  { page: 'composing', id: 'autocomplete', labelKey: 'settingsPanel.toggle.autocomplete', keywords: 'autocomplete recipients suggestions address book' },
  { page: 'composing', id: 'chips', labelKey: 'settingsPanel.toggle.chipRecipients', keywords: 'chips recipients tokens comma' },
  { page: 'composing', id: 'signature-above-quote', labelKey: 'settingsPanel.toggle.signatureAboveQuote', keywords: 'signature footer reply quote above below' },
  { page: 'composing', id: 'vim-editor', labelKey: 'onboarding.extras.vimEditor', keywords: 'vim modal keybindings insert normal' },
  { page: 'composing', id: 'quick-compose', labelKey: 'quickCompose.settings.toggle', keywords: 'quick compose global shortcut hotkey system wide anywhere new message' },
  { page: 'composing', id: 'quick-compose-shortcut', labelKey: 'quickCompose.settings.shortcut', keywords: 'quick compose shortcut hotkey key record' },
  { page: 'composing', id: 'warn-missing-attachment', labelKey: 'sendCheck.setting.attachment', keywords: 'attachment forgot missing attached warn reminder check send file' },
  { page: 'composing', id: 'warn-empty-subject', labelKey: 'sendCheck.setting.subject', keywords: 'subject empty missing warn check send' },
  { page: 'composing', id: 'undo-send', labelKey: 'settingsPanel.label.undoSendWindow', keywords: 'undo send delay hold outgoing' },
  { page: 'composing', id: 'after-reply-send', labelKey: 'sendVariant.setting.afterReply', keywords: 'send archive reply forward done conversation after auto' },
  { page: 'composing', id: 'send-remind-after', labelKey: 'sendVariant.setting.remindAfter', keywords: 'send remind reminder follow up interval default nobody replies' },
]

export default entries
