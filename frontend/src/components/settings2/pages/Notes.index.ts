import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'notes', id: 'notes-vault', labelKey: 'notesSettings.vault', keywords: 'notes folder vault obsidian markdown location directory' },
  { page: 'notes', id: 'notes-templates', labelKey: 'notesSettings.templates', keywords: 'notes templates folder' },
  { page: 'notes', id: 'notes-attachments', labelKey: 'notesSettings.attachments', keywords: 'notes attachments images pasted folder' },
  { page: 'notes', id: 'notes-projects', labelKey: 'notesSettings.projects', keywords: 'notes projects project folder' },
  { page: 'notes', id: 'notes-daily-folder', labelKey: 'notesSettings.dailyFolder', keywords: 'notes daily journal folder' },
  { page: 'notes', id: 'notes-daily-format', labelKey: 'notesSettings.dailyFormat', keywords: 'notes daily journal date format name' },
  { page: 'notes', id: 'notes-daily-template', labelKey: 'notesSettings.dailyTemplate', keywords: 'notes daily journal template agenda' },
  { page: 'notes', id: 'notes-editor-mode', labelKey: 'notesSettings.mode', keywords: 'notes editor live preview source reading mode' },
  { page: 'notes', id: 'notes-line-width', labelKey: 'notesSettings.width', keywords: 'notes line width readable column' },
]

export default entries
