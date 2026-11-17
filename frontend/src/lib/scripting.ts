// scripting.ts holds the types of the automations system and the pure helpers
// around them. The registry (triggers, conditions, actions, modules, value
// types, permissions) comes from the backend as data, so nothing here knows
// any particular trigger or action: pickers, the API reference and the block
// editor are all built from a Catalog.
//
// The block tree types mirror internal/scripting/blocks.go exactly; a block
// script is stored as that tree's JSON and compiled to Lua by the backend.

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
  lua: string
}

/** A step a block script can take. */
export interface ScriptActionDef extends RegistryMeta {
  params: ScriptParam[]
  outputs?: ScriptField[]
  permission?: string
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
}

/** Something a script must declare and the user approve. */
export interface ScriptPermissionDef extends RegistryMeta {
  risk: 'low' | 'medium' | 'high'
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
export type RunStepKind = 'call' | 'planned' | 'read' | 'check' | 'log' | 'denied' | 'declined'

/** One thing a run did. */
export interface RunStep {
  kind: RunStepKind
  block?: string
  function?: string
  summary: string
  level?: string
  error?: string
  result?: boolean
  atMs: number
}

/** How a run ended. */
export type RunStatus = 'running' | 'ok' | 'stopped' | 'error' | 'denied' | 'timeout' | 'limit' | 'skipped'

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
}

/** A problem with one block. */
export interface CompileError {
  block: string
  message: string
}

/** Lua compiled from a block tree. */
export interface CompileResult {
  lua: string
  errors: CompileError[]
  permissions: string[]
}

/** A risky call waiting for the user's yes. */
export interface ScriptConfirmRequest {
  id: string
  scriptId: number
  scriptName: string
  function: string
  summary: string
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

/** A script file read for import, before anything is stored. */
export interface ScriptFile {
  script: Script
  text: string
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

/** The risky functions a script's declared permissions let it call, for "allow without asking". */
export function riskyFunctions(catalog: ScriptCatalog, declared: string[]): string[] {
  const out: string[] = []
  for (const m of catalog.modules) {
    for (const f of m.functions) {
      if (f.risky && (!f.permission || declared.includes(f.permission))) {
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

/** Whether two inputs differ, for the editor's unsaved state. */
export function inputsDiffer(a: ScriptInput, b: ScriptInput): boolean {
  return JSON.stringify(a) !== JSON.stringify(b)
}
