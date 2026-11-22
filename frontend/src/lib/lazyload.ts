// parts of the main window that are kept out of the startup bundle. The window
// loads them once it has settled, so they are ready before they are first used.
//
// A part that shows text outside the core strings names the areas of
// stringareas.ts it needs, and its loader resolves only once those strings are
// in as well, so it never renders before its text.

import { loadStrings } from './i18n'
import type { StringArea } from './stringareas'

/** withStrings wraps a loader so it also loads the string areas the module shows. */
export function withStrings<T>(areas: StringArea[], load: () => Promise<T>): () => Promise<T> {
  return () => Promise.all([load(), loadStrings(areas)]).then(([module]) => module)
}

/** loadComposer loads the New design's composer. */
export const loadComposer = withStrings(['composer'], () => import('../components/compose/Composer2.svelte'))

/** loadClassicCompose loads the Classic design's compose window. */
export const loadClassicCompose = withStrings(['composer'], () => import('../components/compose/Compose.svelte'))

/** loadAttachmentsBrowser loads the New design's attachments browser. */
export const loadAttachmentsBrowser = withStrings(['attachments'], () => import('../components/attachments/AttachmentsBrowser.svelte'))

/** loadMenuBarEditor loads the in-place editor of the in-app menu bar. */
export const loadMenuBarEditor = () => import('../components/common/MenuBarEditor.svelte')

/** loadPromiseStrip loads the reader's strip of promises found in a message. */
export const loadPromiseStrip = withStrings(['dialogs'], () => import('../components/detail/PromiseStrip.svelte'))

/** loadEncryptionIndicator loads the reader's line about an encrypted or signed message. */
export const loadEncryptionIndicator = withStrings(['dialogs'], () => import('../components/detail/EncryptionIndicator.svelte'))

/** loadInviteCard loads the reader's card for a calendar invitation. */
export const loadInviteCard = withStrings(['dialogs'], () => import('../components/detail/InviteCard.svelte'))

/** loadDatePeek loads the preview a date in a mail opens. */
export const loadDatePeek = withStrings(['dialogs'], () => import('../components/detail/DatePeek.svelte'))

/** loadTrustLine loads the reader's line about who sent a message. */
export const loadTrustLine = withStrings(['passport'], () => import('../components/detail/TrustLine.svelte'))

/** loadImportDialog loads the dialog that imports mail from another client. */
export const loadImportDialog = withStrings(['import'], () => import('../components/import/ImportDialog.svelte'))

/** loadGuidedImport loads the New design's guided import from another program. */
export const loadGuidedImport = withStrings(['import'], () => import('../components/import/GuidedImport.svelte'))

/** loadAddMailboxWizard loads the wizard that adds a mailbox. */
export const loadAddMailboxWizard = withStrings(['wizard'], () => import('../components/wizard/AddMailboxWizard.svelte'))

/** loadSettingsPanel loads the Classic design's settings. */
export const loadSettingsPanel = withStrings(['settings'], () => import('../components/settings/SettingsPanel.svelte'))

/** loadSettingsScreen loads the New design's settings. */
export const loadSettingsScreen = withStrings(['settings'], () => import('../components/settings2/SettingsScreen.svelte'))

/** loadCliCard loads the New design's command line access settings. */
export const loadCliCard = withStrings(['settings'], () => import('../components/settings2/CliCard.svelte'))

/** loadOnboarding loads the first-run onboarding. */
export const loadOnboarding = withStrings(['onboarding', 'wizard'], () => import('../components/onboarding/Onboarding.svelte'))

/** loadSetup loads the setup flow that can be run again from settings. */
export const loadSetup = withStrings(['onboarding', 'wizard'], () => import('../components/onboarding/Setup.svelte'))

/** loadAutomationsSpace loads the automations space. */
export const loadAutomationsSpace = withStrings(['automations'], () => import('../components/automations/AutomationsSpace.svelte'))

/** loadAutomationsOverlay loads the automations editor shown over another space. */
export const loadAutomationsOverlay = withStrings(['automations'], () => import('../components/automations/AutomationsOverlay.svelte'))

/** loadCalendarScreen loads the calendar space. */
export const loadCalendarScreen = withStrings(['calendar'], () => import('../components/calendar/CalendarScreen.svelte'))

/** loadMeetingPrep loads the preparation sheet for a meeting. */
export const loadMeetingPrep = withStrings(['calendar'], () => import('../components/calendar/MeetingPrep.svelte'))

/** loadChatSpace loads the chat space. */
export const loadChatSpace = withStrings(['chat'], () => import('../components/chat/ChatSpace.svelte'))

/** loadShareToChatDialog loads the dialog that shares a message to a chat. */
export const loadShareToChatDialog = withStrings(['chat'], () => import('../components/chat/ShareToChatDialog.svelte'))

/** loadVerificationDialog loads the dialog that verifies a chat device. */
export const loadVerificationDialog = withStrings(['chat'], () => import('../components/chat/VerificationDialog.svelte'))

/** loadContactsSpace loads the contacts space. */
export const loadContactsSpace = withStrings(['contacts'], () => import('../components/contacts/ContactsSpace.svelte'))

/** loadFeedsScreen loads the feeds space. */
export const loadFeedsScreen = withStrings(['feeds'], () => import('../components/feeds/FeedsScreen.svelte'))

/** loadNotesScreen loads the notes space. */
export const loadNotesScreen = withStrings(['notes'], () => import('../components/notes/NotesScreen.svelte'))

/** loadProjectsScreen loads the projects space. */
export const loadProjectsScreen = withStrings(['projects'], () => import('../components/projects/ProjectsScreen.svelte'))

/** loadTasksScreen loads the tasks space. */
export const loadTasksScreen = withStrings(['tasks'], () => import('../components/tasks/TasksScreen.svelte'))

/** loadNewsletterManager loads the newsletter overview. */
export const loadNewsletterManager = withStrings(['newsletters'], () => import('../components/newsletters/NewsletterManager.svelte'))

/** loadVaultDialogs loads the mail vault's dialogs. */
export const loadVaultDialogs = withStrings(['vault'], () => import('../components/vault/VaultDialogs.svelte'))

/** loadNightlyDialog loads the notice shown on a nightly build. */
export const loadNightlyDialog = withStrings(['dialogs'], () => import('../components/common/NightlyDialog.svelte'))

/** loadSyncFailureDialog loads the dialog that explains a failed sync. */
export const loadSyncFailureDialog = withStrings(['dialogs'], () => import('../components/common/SyncFailureDialog.svelte'))

/** loadAccountPasswordDialog loads the dialog that asks for a mailbox's password. */
export const loadAccountPasswordDialog = withStrings(['dialogs'], () => import('../components/settings/AccountPasswordDialog.svelte'))

/** warmUp runs each loader once the window is idle, ignoring failures. */
export function warmUp(loaders: (() => Promise<unknown>)[]): void {
  const load = () => {
    for (const loader of loaders) {
      void loader().catch(() => undefined)
    }
  }
  const w = window as Window & { requestIdleCallback?: (cb: () => void, opts?: { timeout: number }) => number }
  if (w.requestIdleCallback) {
    w.requestIdleCallback(load, { timeout: 2000 })
  } else {
    setTimeout(load, 300)
  }
}
