// stringareas.ts splits the interface text into areas. Core is loaded at
// startup; every other area is loaded together with the code that shows it
// (see lazyload.ts), so a window only carries the text it can show.
//
// A key belongs to the area of the longest prefix listed below that it starts
// with, matched on whole dot-separated parts. The catalogs in locales/ stay one
// file per language; the build splits each into these areas. strings.test.ts
// checks that every key has an area and that each area's keys are only used
// where that area is loaded.
//
// Keys under scripts.perm, scripts.trigger and scripts.result are named by the
// backend's script catalog and shown by the always-loaded automation dialogs,
// so they stay in core.

/** One part of the interface text that loads as a whole. */
export type StringArea =
  | 'core'
  | 'settings'
  | 'automations'
  | 'onboarding'
  | 'wizard'
  | 'import'
  | 'composer'
  | 'calendar'
  | 'chat'
  | 'contacts'
  | 'feeds'
  | 'notes'
  | 'projects'
  | 'tasks'
  | 'newsletters'
  | 'attachments'
  | 'passport'
  | 'vault'
  | 'outbox'
  | 'dialogs'

/** The key prefixes of each area. */
export const stringAreas: Record<StringArea, string[]> = {
  core: [
    'a11y', 'action', 'addMailbox', 'agent', 'ai', 'app', 'applock', 'attachments.kind',
    'attachments.palette', 'attachments.title', 'autoEncrypt.lock.verified', 'autoEncrypt.pref',
    'autoEncrypt.problem', 'autoEncrypt.recipient.changed', 'autoEncrypt.source', 'avail.keywords',
    'avail.open', 'caldav', 'calendar', 'calendarSettings.midnight', 'calendarSettings.workEnd',
    'calendarSettings.workStart', 'calendarSettings.workingHours', 'certs', 'chat.new.title',
    'chat.share.action', 'chat.share.keywords', 'chat.signin', 'chat.verify.thisDevice',
    'chatSettings.enabled', 'coach', 'common', 'compose', 'confirm', 'contacts', 'crash',
    'dateHint.mark', 'design', 'detail', 'export', 'feeds', 'flagColor', 'focus', 'folders',
    'followUp', 'history', 'import.entry', 'import.files.title', 'importExport', 'inboxZero',
    'insights.duration', 'insights.keywords', 'language.custom', 'layout', 'liability', 'links',
    'list.header', 'list.selectAll', 'mailboxes.cancel', 'mailboxes.colorHint',
    'mailboxes.export.failed', 'mailboxes.fromNameHint', 'mailboxes.localLabel',
    'mailboxes.localLabelHint', 'mailboxes.passwordPrompt.marker', 'mailboxes.route',
    'mailboxes.save', 'mailvault', 'menu', 'menuBar', 'messageList', 'modal', 'mute', 'myday',
    'nav', 'navHistory', 'network', 'newsletters.freq', 'newsletters.keywords', 'newsletters.title',
    'note', 'notes', 'onboarding.avatar', 'onboarding.continue', 'onboarding.density',
    'onboarding.row', 'onboarding.theme', 'outbox', 'palette', 'panes', 'passport', 'phishing',
    'pin', 'prep.button', 'prep.next', 'privacy', 'profiles', 'projects.addTo',
    'projects.addToCount', 'projects.archived', 'projects.card.removed', 'projects.chip',
    'projects.compose', 'projects.drop', 'projects.hint', 'projects.mail.addedMany',
    'projects.mail.addedOne', 'projects.new', 'projects.noMatches', 'projects.open',
    'projects.pickPlaceholder', 'projects.tab', 'promises', 'quickAdd', 'quickFilter', 'rail',
    'reader', 'reading', 'recipients', 'rowActions', 'scripts.ask', 'scripts.assistant.review',
    'scripts.assistant.toast.created', 'scripts.assistant.toast.proposed',
    'scripts.assistant.toast.updated', 'scripts.blocks.enableBlocked',
    'scripts.blocks.regex.backref', 'scripts.blocks.regex.invalid',
    'scripts.blocks.regex.lookaround', 'scripts.blocks.rule.name',
    'scripts.blocks.rule.unavailable', 'scripts.blocks.ruleFromMail', 'scripts.cancel',
    'scripts.catchUpAsk', 'scripts.code', 'scripts.confirm', 'scripts.error',
    'scripts.gallery.created', 'scripts.history', 'scripts.import', 'scripts.importDialog',
    'scripts.mark', 'scripts.menu', 'scripts.new', 'scripts.perm', 'scripts.permissions',
    'scripts.progress', 'scripts.result', 'scripts.risk', 'scripts.status', 'scripts.toast',
    'scripts.trigger', 'search', 'sendVariant', 'senderPanel', 'settings', 'settings2.page.filters',
    'settings2.page.insights', 'settings2.page.vacation', 'settingsPanel.accentLabel',
    'settingsPanel.accentPresets', 'settingsPanel.bodyFont', 'settingsPanel.button.cancel',
    'settingsPanel.category', 'settingsPanel.group', 'settingsPanel.keywords',
    'settingsPanel.label.darkFrom', 'settingsPanel.label.darkUntil', 'settingsPanel.label.density',
    'settingsPanel.label.interfaceScale', 'settingsPanel.label.rowLayout',
    'settingsPanel.label.theme', 'settingsPanel.monoFont', 'settingsPanel.scale',
    'settingsPanel.theme', 'settingsPanel.toggle.showMessagePreview', 'settingsPanel.uiFont',
    'setup.palette', 'setup.runAgain', 'shield.links', 'shield.trackers', 'shield.unsub',
    'shortcut', 'shortcuts', 'sidebar', 'sieve.palette', 'sieve.problem', 'signatures.cancel',
    'snippets', 'space', 'spaces', 'spam', 'split', 'syncFailure.marker', 'tabs', 'tasks',
    'themes.baseLockedHint', 'thread', 'timeblock.palette', 'timeblock.toast', 'tips', 'topbar', 'trust', 'undoToast', 'views', 'vip',
    'virustotal', 'whatsnew', 'window', 'wizard.advanced.imapSecurity',
    'wizard.advanced.smtpSecurity', 'wizard.back', 'wizard.done.button', 'wizard.field.fromName',
    'wizard.field.imapHost', 'wizard.field.localLabel', 'wizard.field.password',
    'wizard.field.port', 'wizard.field.smtpHost', 'wizard.field.username', 'wizard.start',
    'wizard.testing',
  ],
  settings: [
    'about', 'addressBook', 'applock.settings', 'autoEncrypt', 'calendarSettings', 'chat.setup',
    'chat.unlock', 'chatSettings', 'cli', 'composing', 'encryption', 'external', 'feedSettings',
    'iconPicker', 'imageAllowlist', 'import.tb.title', 'insights', 'language', 'list', 'mailboxes',
    'mcp', 'nightly.appName', 'notesSettings', 'notify', 'onboarding.accentTitle',
    'onboarding.extras', 'onboarding.flagopt', 'onboarding.font', 'preview', 'projects.settings',
    'promiseSettings', 'quickCompose.settings', 'sendCheck.setting', 'settings.list', 'settings2',
    'settingsPanel', 'sieve', 'signatures', 'swipe', 'taskSettings', 'themeEditor', 'themes',
  ],
  automations: ['scripts'],
  onboarding: ['avatarPlacement', 'onboarding', 'onboarding.extras.avatarStyle', 'setup'],
  wizard: ['wizard'],
  import: ['import'],
  composer: [
    'autoEncrypt.bccNote', 'autoEncrypt.changed', 'autoEncrypt.downgrade', 'autoEncrypt.lock',
    'autoEncrypt.notices', 'autoEncrypt.recipient', 'autoEncrypt.summary', 'autoEncrypt.toggle',
    'avail', 'compose.mode', 'compose.protection', 'compose2', 'quickCompose', 'sendCheck',
  ],
  calendar: ['calendar.conflict', 'calendar.sched', 'calendar.scope', 'prep', 'timeblock'],
  chat: ['chat'],
  contacts: ['contacts.duplicates', 'contacts.merge', 'contacts.sort'],
  feeds: ['feeds.add', 'feeds.empty', 'feeds.folder', 'feeds.reader', 'feeds.settings'],
  notes: ['notes.empty', 'notes.graph', 'notes.menu'],
  projects: ['projects'],
  tasks: ['tasks.conflict', 'tasks.empty', 'tasks.emptyDetail'],
  newsletters: ['newsletters'],
  attachments: ['attachments'],
  passport: ['passport.change', 'passport.history', 'passport.what', 'shield'],
  vault: ['mailvault.recovery', 'mailvault.temp'],
  outbox: ['outbox.log'],
  dialogs: [
    'autoEncrypt.reader', 'autoEncrypt.setup', 'dateHint', 'invite', 'mailboxes.passwordPrompt',
    'nightly', 'promises.edit', 'syncFailure',
  ],
}

let byPrefix: Map<string, StringArea> | null = null

/** areaOf names the area a key belongs to, or null when no listed prefix matches it. */
export function areaOf(key: string): StringArea | null {
  if (!byPrefix) {
    byPrefix = new Map()
    for (const [area, prefixes] of Object.entries(stringAreas) as [StringArea, string[]][]) {
      for (const prefix of prefixes) {
        byPrefix.set(prefix, area)
      }
    }
  }
  for (let end = key.length; end > 0; end = key.lastIndexOf('.', end - 1)) {
    const area = byPrefix.get(key.slice(0, end))
    if (area) {
      return area
    }
  }
  return null
}
