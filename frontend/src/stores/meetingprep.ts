// meetingprep.ts holds which meeting the Prepare panel is open for. Anything
// can open it, the event popover, the editor, My Day or a reminder, and the app
// shows it above whatever is on screen.

import { writable } from 'svelte/store'

/** The meeting the Prepare panel is for, or null while it is closed. */
export const meetingPrepTarget = writable<{ eventId: number; recurrenceId: string } | null>(null)

/** Opens the Prepare panel for an event, or for one occurrence of a series. */
export function openMeetingPrep(eventId: number, recurrenceId = ''): void {
  meetingPrepTarget.set({ eventId, recurrenceId })
}

/** Closes the Prepare panel. */
export function closeMeetingPrep(): void {
  meetingPrepTarget.set(null)
}
