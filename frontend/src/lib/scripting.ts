// scripting.ts holds the types of the automations system and the pure helpers
// around them. The registry (triggers, conditions, actions, modules, value
// types, permissions) comes from the backend as data, so nothing here knows
// any particular trigger or action: pickers, the API reference and the block
// editor are all built from a Catalog.
//
// The block tree types mirror internal/scripting/blocks.go exactly; a block
// script is stored as that tree's JSON and compiled to Lua by the backend.

import { aiTarget, permCloud } from './ai'

/** How a script is written. A block script compiles to Lua; Lua never turns back into blocks. */
export type ScriptKind = 'lua' | 'blocks'

/** Where a script came from. Imported and assistant scripts start switched off. */
export type ScriptOrigin = 'user' | 'import' | 'assistant'

/** What a mail script does with mail that arrived while Pelton was closed. */
export type CatchUpMode = 'run' | 'live' | 'ask'

/** The type of a parameter, which decides its control and how it compiles. */
export type ParamType =
  | 'string'
  | 'text'
  | 'number'
  | 'bool'
  | 'enum'
  | 'regex'
  | 'duration'
  | 'time'
  | 'folder'
  | 'account'
  | 'color'
  | 'address'
  | 'list'
  | 'ref'
  | 'icon'

/** One choice of an enum parameter. */
export interface ParamOption {
  value: string
  labelKey: string
  label?: string
}

/** A parameter of a trigger, condition, action, function or script setting. */
export interface ScriptParam {
  name: string
  type: ParamType
  labelKey: string
  label?: string
  descKey?: string
  /** The English description shown when descKey has no string. */
  description?: string
  required?: boolean
  default?: unknown
  /** A reference used when no value is given, e.g. "event.message". */
  defaultRef?: string
  options?: ParamOption[]
  /** The element type of a list. */
  of?: ParamType
  /** The value type a reference should point at, e.g. "message". */
  valueType?: string
}

/** A named, typed value: an event field, an action output, a function result. */
export interface ScriptField {
  name: string
  type: string
  labelKey: string
  label?: string
  /** The element type of a list field: a param type or a value type id. */
  of?: string
}

/** Which condition tests a field of a value type, e.g. "subject" by "mail.subject". */
export interface FieldCondition {
  /** The field's path, e.g. "headers.list-id". */
  field: string
  condition: string
  /** The condition parameter the field's value goes into. */
  param: string
  /** Fixed arguments, e.g. a header name. */
  args?: Record<string, unknown>
  /** Separates a field holding several values; a rule matches on the first. */
  split?: string
  /** What the match is called, e.g. "Sender". */
  labelKey: string
  label?: string
}

/** What every registry entry carries to be shown and found. */
export interface RegistryMeta {
  id: string
  labelKey: string
  descKey: string
  label?: string
  description?: string
  category: string
}

/** Something that starts a script. */
export interface ScriptTriggerDef extends RegistryMeta {
  params: ScriptParam[]
  event: ScriptField[]
  permission?: string
  mutable?: boolean
  /** Set when the app has no hook for this trigger yet; it never fires. */
  missing?: string
}

/** A yes or no question a block script can ask. */
export interface ScriptConditionDef extends RegistryMeta {
  params: ScriptParam[]
  permission?: string
  /** Every permission it needs, permission first. */
  permissions?: string[]
  lua: string
  /** The message field a text condition tests, for the match tester. */
  field?: string
}

/** A step a block script can take. */
export interface ScriptActionDef extends RegistryMeta {
  params: ScriptParam[]
  outputs?: ScriptField[]
  permission?: string
  /** Every permission it needs, permission first. */
  permissions?: string[]
  risky?: boolean
  lua: string
  /** The triggers the action works with; empty means all. */
  triggers?: string[]
  /** Set when the app lacks what the action needs; the compiler refuses it. */
  missing?: string
}

/** A host function of a Lua module. */
export interface ScriptFunctionDef {
  name: string
  descKey: string
  description?: string
  params: ScriptParam[]
  returns?: ScriptField[]
  permission?: string
  /** Every permission it needs, permission first. */
  permissions?: string[]
  risky?: boolean
  effect?: boolean
}

/** A Lua module, a global table of host functions. */
export interface ScriptModuleDef extends RegistryMeta {
  functions: ScriptFunctionDef[]
}

/** A structured value a script handles, e.g. a message. */
export interface ScriptValueTypeDef extends RegistryMeta {
  fields: ScriptField[]
  lazy?: Record<string, string>
  /** The conditions that test the type's fields. */
  conditions?: FieldCondition[]
}

/** Something a script must declare and the user approve. */
export interface ScriptPermissionDef extends RegistryMeta {
  risk: 'low' | 'medium' | 'high'
}

/** A question a running script can ask; the dialog draws it by control. */
export interface ScriptInputKindDef extends RegistryMeta {
  control: string
  /** The type of the answer. */
  value: string
  params?: ScriptParam[]
}

/** A button of the result panel. client names a window handler; without it the backend carries it out. */
export interface ScriptResultActionDef extends RegistryMeta {
  icon: string
  client?: string
  permission?: string
}

/** A place in the app a script can be started from by hand. */
export interface ScriptPlacementDef extends RegistryMeta {
  /** menu, palette, toolbar, shortcut or row. */
  surface: string
  /** The value type handed over, empty for none; optional means only when there is one. */
  valueType?: string
  optional?: boolean
  /** The event field a selection of several arrives in. */
  list?: string
  permission?: string
  /** What a script sets for the place, e.g. an icon and a label. */
  options?: ScriptParam[]
}

/** The whole registry as data. */
export interface ScriptCatalog {
  triggers: ScriptTriggerDef[]
  conditions: ScriptConditionDef[]
  actions: ScriptActionDef[]
  modules: ScriptModuleDef[]
  valueTypes: ScriptValueTypeDef[]
  permissions: ScriptPermissionDef[]
  paramTypes: ParamType[]
  /** The types a script setting may have. */
  settingTypes: ParamType[]
  inputKinds: ScriptInputKindDef[]
  resultActions: ScriptResultActionDef[]
  placements: ScriptPlacementDef[]
  /** The ready made scripts of the gallery. */
  templates?: ScriptTemplateDef[]
}

/** A ready made script the user copies from the gallery. */
export interface ScriptTemplateDef extends RegistryMeta {
  kind: ScriptKind
  trigger: { type: string; params?: Record<string, unknown> }
  blocks?: BlockTree
  source?: string
  settings?: ScriptParam[]
  /** What it declares and what its blocks need. */
  permissions: string[]
  /** Whether one of those sends data to an AI provider. */
  ai: boolean
  /** The libraries it requires. */
  requires?: string[]
  /** Why it would not compile now, e.g. an AI step without a provider. */
  missing?: string
}

/** A Lua module the user writes once and loads from scripts with require("name"). */
export interface ScriptLibrary {
  id: number
  name: string
  description: string
  source: string
  origin: ScriptOrigin
  createdAt: string
  updatedAt: string
}

/** What the editor saves of a library. */
export interface ScriptLibraryInput {
  name: string
  description: string
  source: string
}

/** Asks which recent messages of a folder a pattern or a condition matches. */
export interface MatchRequest {
  folder: string
  accountId: number
  limit: number
  /** An RE2 expression tried on field. */
  pattern: string
  /** subject, from, to, body or headers.<name>. */
  field: string
  /** A condition or group; with it a message matches when the condition holds. */
  when?: BlockCond
}

/** One message a match test matched; spans are UTF-16 offsets into text. */
export interface MatchHit {
  messageId: number
  subject: string
  from: string
  date: number
  text?: string
  spans: [number, number][]
}

/** What a match test found. */
export interface MatchReport {
  scanned: number
  matched: number
  matches: MatchHit[]
  timedOut: boolean
}

/** A place a script is offered at, with its options. */
export interface PlacementConfig {
  id: string
  options?: Record<string, unknown>
}

/** A script offered at a placement. */
export interface PlacedScript {
  scriptId: number
  name: string
  description: string
  options: Record<string, unknown>
}

/** One value a placement hands a script: an id to load, or the value's fields. */
export interface PlacedItem {
  id: string
  fields?: Record<string, unknown>
}

/** A question a running script puts to the user. */
export interface ScriptAsk {
  id: string
  scriptId: number
  scriptName: string
  kind: string
  control: string
  prompt: string
  params: Record<string, unknown>
  default: unknown
  dry: boolean
  attended: boolean
  /** How long it waits for a script nobody started; 0 for as long as it takes. */
  timeoutSeconds: number
}

/** Longer output a script shows in the result panel. */
export interface ScriptResult {
  id: string
  scriptId: number
  scriptName: string
  title: string
  text: string
  format: 'text' | 'markdown'
  /** The result actions on offer, by id. */
  actions: string[]
  dry: boolean
}

/** A run in progress; id is not the run log id. */
export interface LiveRun {
  id: number
  scriptId: number
  scriptName: string
  dry: boolean
  attended: boolean
  steps: number
  last: string
  waiting: boolean
  done: boolean
}

/** A parameter value: a literal, or a reference ("event.message", "var:x", "step:b3.id", "setting:name"). */
export interface BlockArg {
  value?: unknown
  ref?: string
}

/** The kinds of block. */
export type BlockKind = 'action' | 'if' | 'foreach' | 'set' | 'stop' | 'lua'

/** One step of a block script; which fields apply depends on kind. */
export interface Block {
  /** Unique within the tree; later blocks refer to an action's outputs as step:<id>.<field>. */
  id: string
  kind: BlockKind
  /** The action id, for an action block. */
  type?: string
  args?: Record<string, BlockArg>
  /** An if block's branches: the first is the if, the rest else if. */
  branches?: BlockBranch[]
  else?: Block[]
  /** A for each block's list. */
  list?: BlockArg
  /** The variable a for each or set block writes. */
  as?: string
  /** A set block's value. */
  value?: BlockArg
  /** A for each block's body. */
  body?: Block[]
  /** A Lua block's code. */
  code?: string
}

/** One condition and the blocks it guards. */
export interface BlockBranch {
  when: BlockCond
  body: Block[]
}

/** A condition: a registered one with args, or an all, any or not group. A not has one item. */
export interface BlockCond {
  kind: 'all' | 'any' | 'not' | 'condition'
  type?: string
  args?: Record<string, BlockArg>
  items?: BlockCond[]
}

/** A block script. */
export interface BlockTree {
  version: number
  blocks: Block[]
}

/** The version of the block tree format. */
export const blockTreeVersion = 1

/** A script's trigger and its parameters. */
export interface ScriptTriggerConfig {
  type: string
  params: Record<string, unknown>
}

/** One script as the backend returns it. blocks is the tree as JSON text. */
export interface Script {
  id: number
  name: string
  description: string
  kind: ScriptKind
  source: string
  blocks: string
  trigger: ScriptTriggerConfig
  permissions: string[]
  approved: string[]
  enabled: boolean
  origin: ScriptOrigin
  settingsSchema: ScriptParam[]
  settings: Record<string, unknown>
  catchUp: CatchUpMode
  catchUpCap: number
  allowUnasked: string[]
  /** Where in the app the script is offered. */
  placements: PlacementConfig[]
  createdAt: string
  updatedAt: string
  lastRunAt: string
}

/** What the editor saves. */
export interface ScriptInput {
  name: string
  description: string
  kind: ScriptKind
  source: string
  blocks: string
  trigger: ScriptTriggerConfig
  permissions: string[]
  settingsSchema: ScriptParam[]
  catchUp: CatchUpMode
  catchUpCap: number
}

/** What a step of a run is. */
export type RunStepKind = 'call' | 'planned' | 'read' | 'check' | 'log' | 'denied' | 'declined' | 'ask'

/** One thing a run did. */
export interface RunStep {
  kind: RunStepKind
  block?: string
  function?: string
  summary: string
  level?: string
  error?: string
  result?: boolean
  /** The index of the if block's branch a check belongs to. */
  branch?: number
  atMs: number
}

/** How a run ended. */
export type RunStatus = 'running' | 'ok' | 'stopped' | 'error' | 'denied' | 'timeout' | 'limit' | 'skipped' | 'cancelled'

/** One run, live or from the log. */
export interface ScriptRun {
  id: number
  scriptId: number
  scriptName: string
  trigger: string
  dry: boolean
  depth: number
  started: string
  durationMs: number
  status: RunStatus
  error: string
  steps: RunStep[]
  changes: number
  cancelled: boolean
  cancelReason: string
  logTruncated: boolean
  undoable: boolean
  messages: number[]
  /** The Lua line a failed run stopped on, 0 when unknown. */
  errorLine: number
  /** The block of a block script that was running when it failed. */
  errorBlock: string
  /** The library errorLine counts in, '' for the script itself. */
  errorLibrary?: string
  /** The blocks of a block script the run never got to. */
  notReached: string[]
}

/** A problem with one block. */
export interface CompileError {
  block: string
  message: string
}

/** The first and last line of a block in compiled Lua, counted from 1. */
export interface LineRange {
  from: number
  to: number
}

/** Lua compiled from a block tree. */
export interface CompileResult {
  lua: string
  errors: CompileError[]
  permissions: string[]
  /** The lines each block became, nested blocks inside their parent's range. */
  lines: Record<string, LineRange>
}

/** A risky call waiting for the user's yes. */
export interface ScriptConfirmRequest {
  id: string
  scriptId: number
  scriptName: string
  function: string
  summary: string
  /** Asked every time, e.g. a call an AI model chose or changed recipients; offers no "allow without asking". */
  everyTime?: boolean
}

/** A script asking whether to run over mail that arrived while Pelton was closed. */
export interface ScriptCatchUp {
  scriptId: number
  name: string
  count: number
}

/** A message a script shows. */
export interface ScriptToast {
  script: string
  text: string
  level: string
}

/** A script or library file read for import, before anything is stored. */
export interface ScriptFile {
  script: Script
  /** The library a file of kind "library" holds. */
  library: ScriptLibrary | null
  kind: string
  text: string
  /** The libraries the file requires that do not exist here. */
  missingLibraries: string[]
}

/** The file extension of an exported script. */
export const scriptExtension = '.peltonscript'

/** An empty block tree. */
export function emptyTree(): BlockTree {
  return { version: blockTreeVersion, blocks: [] }
}

/** Reads a script's block tree, or an empty one when it has none or it does not parse. */
export function parseTree(json: string): BlockTree {
  if (!json.trim()) {
    return emptyTree()
  }
  try {
    const tree = JSON.parse(json) as BlockTree
    return { version: tree.version || blockTreeVersion, blocks: Array.isArray(tree.blocks) ? tree.blocks : [] }
  } catch {
    return emptyTree()
  }
}

/** Writes a block tree as the JSON the backend stores. */
export function serializeTree(tree: BlockTree): string {
  return JSON.stringify(tree)
}

/** The editable part of a script. */
export function toInput(s: Script): ScriptInput {
  return {
    name: s.name,
    description: s.description,
    kind: s.kind,
    source: s.source,
    blocks: s.blocks,
    trigger: { type: s.trigger.type, params: { ...s.trigger.params } },
    permissions: [...s.permissions],
    settingsSchema: s.settingsSchema,
    catchUp: s.catchUp,
    catchUpCap: s.catchUpCap,
  }
}

/** A new script's starting point. */
export function newInput(kind: ScriptKind, name: string): ScriptInput {
  return {
    name,
    description: '',
    kind,
    source: kind === 'lua' ? '-- event holds what started the run\nlog.info("hello from", script.name)\n' : '',
    blocks: kind === 'blocks' ? serializeTree(emptyTree()) : '',
    trigger: { type: 'manual', params: {} },
    permissions: [],
    settingsSchema: [],
    catchUp: 'live',
    catchUpCap: 50,
  }
}

/** Resolves an entry's label: its localization, its English fallback, or its id. */
export function entryLabel(t: (key: string) => string, e: { labelKey: string; label?: string; id?: string; name?: string }): string {
  const s = e.labelKey ? t(e.labelKey) : ''
  if (s && s !== e.labelKey) {
    return s
  }
  return e.label || e.id || e.name || ''
}

/** Resolves an entry's description the same way, or ''. */
export function entryDescription(t: (key: string) => string, e: { descKey?: string; description?: string }): string {
  const s = e.descKey ? t(e.descKey) : ''
  if (s && s !== e.descKey) {
    return s
  }
  return e.description ?? ''
}

/** The permissions a trigger needs, with what the script already declares added. */
export function withTriggerPermission(catalog: ScriptCatalog, triggerId: string, declared: string[]): string[] {
  const need = catalog.triggers.find((t) => t.id === triggerId)?.permission
  return need && !declared.includes(need) ? [...declared, need] : declared
}

/** The declared permissions the user has not approved yet. */
export function unapproved(s: Pick<Script, 'permissions' | 'approved'>): string[] {
  return s.permissions.filter((p) => !s.approved.includes(p))
}

/** The risk of a declared permission, by its base id ("net:x.org" finds "net"). */
export function permissionDef(catalog: ScriptCatalog, id: string): ScriptPermissionDef | undefined {
  const base = id.split(':')[0]
  return catalog.permissions.find((p) => p.id === base)
}

/** Every permission an entry needs: its list, or its single permission. */
export function needs(def: { permission?: string; permissions?: string[] } | undefined): string[] {
  if (!def) {
    return []
  }
  if (def.permissions && def.permissions.length > 0) {
    return def.permissions
  }
  return def.permission ? [def.permission] : []
}

/**
 * Whether the declared permissions meet one an entry needs: declared as is,
 * or a base such as "net" met by a targeted form such as "net:example.org".
 * An AI step's "ai" is met by any provider, "ai:<id>" or "ai_local:<id>".
 */
export function permissionMet(perm: string, declared: string[]): boolean {
  if (perm.includes(':')) {
    return declared.includes(perm)
  }
  return declared.some((p) => p === perm || p.startsWith(`${perm}:`) || (perm === permCloud && aiTarget(p) !== ''))
}

/** The risky functions a script's declared permissions let it call, for "allow without asking". */
export function riskyFunctions(catalog: ScriptCatalog, declared: string[]): string[] {
  const out: string[] = []
  for (const m of catalog.modules) {
    for (const f of m.functions) {
      if (f.risky && needs(f).every((p) => permissionMet(p, declared))) {
        out.push(`${m.id}.${f.name}`)
      }
    }
  }
  return out
}

/** Whether a trigger runs on a message, so a test run can pick one. */
export function triggerTakesMessage(catalog: ScriptCatalog, triggerId: string): boolean {
  return catalog.triggers.find((t) => t.id === triggerId)?.event.some((f) => f.type === 'message') ?? false
}

/** The value a manual script takes, 'none' for none. */
export function scriptScope(s: Pick<Script, 'trigger'>): string {
  const scope = s.trigger.params.scope
  return typeof scope === 'string' && scope ? scope : 'none'
}

/** Whether a script's trigger takes what a placement hands over. */
export function placementFits(p: ScriptPlacementDef, s: Pick<Script, 'trigger'>): boolean {
  if (s.trigger.type !== 'manual') {
    return false
  }
  const scope = scriptScope(s)
  if (!p.valueType) {
    return scope === 'none'
  }
  if (p.optional) {
    return scope === 'none' || scope === p.valueType
  }
  return scope === p.valueType
}

/** Whether two inputs differ, for the editor's unsaved state. */
export function inputsDiffer(a: ScriptInput, b: ScriptInput): boolean {
  return JSON.stringify(a) !== JSON.stringify(b)
}

/**
 * Markdown a script wrote with every "<" outside code turned into text, so
 * none of it renders as html: a meta refresh or a form in a result could
 * otherwise load a page, and carry what the script read, without the
 * script's network permission.
 */
export function inertMarkdown(md: string): string {
  let fence = ''
  return md
    .split('\n')
    .map((line) => {
      const marker = /^\s{0,3}(`{3,}|~{3,})/.exec(line)?.[1]
      if (fence) {
        if (marker && marker[0] === fence[0] && marker.length >= fence.length) {
          fence = ''
        }
        return line
      }
      if (marker) {
        fence = marker
        return line
      }
      return line
        .split(/(`+[^`]*`+)/)
        .map((part, i) => (i % 2 === 1 ? part : part.replaceAll('<', '&lt;')))
        .join('')
    })
    .join('\n')
}
