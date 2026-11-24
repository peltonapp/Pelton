// what an assistant connected over MCP did to the user's scripts, and the
// changes it proposed that wait for the user's review.

/** One line of a proposal's diff: ' ' in both versions, '-' only in the current one, '+' only in the proposal. */
export interface ScriptDiffLine {
  op: ' ' | '-' | '+'
  text: string
}

/** A change that takes back a script's approval when an assistant proposed it. */
export type ScriptResetReason = 'code' | 'trigger' | 'permissions' | 'triggerParams' | 'placements'

/** A change an assistant proposed to a script. */
export interface ScriptProposal {
  scriptId: number
  /** False when nothing is waiting. */
  exists: boolean
  when: string
  diff: ScriptDiffLine[]
  addedPermissions: string[]
  removedPermissions: string[]
  /** Accepting switches the script off and clears its approvals. */
  resetsApproval: boolean
  /** What the proposal changes that does so. */
  resetReasons: ScriptResetReason[]
  /** Set when the proposal can no longer be applied; it can still be rejected. */
  problem: string
}

/** What an assistant did to a script, or what the user decided about its proposal. */
export interface AssistantScriptEvent {
  scriptId: number
  name: string
  action: 'created' | 'updated' | 'proposed' | 'accepted' | 'rejected'
}

/** The toast message key for an event, or null when the event needs none. */
export function assistantToastKey(e: AssistantScriptEvent): string | null {
  switch (e.action) {
    case 'created':
    case 'updated':
    case 'proposed':
      return `scripts.assistant.toast.${e.action}`
    default:
      return null
  }
}
