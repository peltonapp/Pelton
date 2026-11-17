// scriptfixtures.ts holds a small registry and a script for the automations
// tests, shaped like what the backend sends.

import type { Script, ScriptCatalog } from '../lib/scripting'

/** A registry with three triggers, the mail module and three permissions. */
export const catalogFixture: ScriptCatalog = {
  triggers: [
    { id: 'manual', labelKey: 'scripts.trigger.manual', descKey: '', category: 'manual', params: [], event: [{ name: 'message', type: 'message', labelKey: '' }] },
    { id: 'mail.arrived', labelKey: 'scripts.trigger.mail.arrived', descKey: '', category: 'mail', params: [], event: [{ name: 'message', type: 'message', labelKey: '' }], permission: 'mail.read' },
    { id: 'app.start', labelKey: 'scripts.trigger.app.start', descKey: '', category: 'app', params: [], event: [] },
  ],
  conditions: [],
  actions: [],
  modules: [
    {
      id: 'mail',
      labelKey: '',
      descKey: '',
      category: 'mail',
      functions: [
        { name: 'move', descKey: '', params: [], permission: 'mail.modify', effect: true },
        { name: 'send', descKey: '', params: [], permission: 'mail.send', effect: true, risky: true },
      ],
    },
  ],
  valueTypes: [],
  permissions: [
    { id: 'mail.read', labelKey: 'scripts.perm.mail.read', descKey: 'scripts.perm.mail.read.desc', category: 'mail', risk: 'low' },
    { id: 'mail.send', labelKey: 'scripts.perm.mail.send', descKey: '', category: 'mail', risk: 'high' },
    { id: 'net', labelKey: 'x.net', descKey: '', label: 'Web requests', category: 'outside', risk: 'high' },
  ],
  paramTypes: ['string'],
  settingTypes: ['string', 'text', 'number', 'bool', 'enum', 'folder', 'account', 'color'],
  inputKinds: [],
  resultActions: [],
  placements: [],
}

const messageParam = { name: 'message', type: 'ref' as const, labelKey: 'scripts.param.message', valueType: 'message', defaultRef: 'event.message' }
const opParam = {
  name: 'op',
  type: 'enum' as const,
  labelKey: 'scripts.param.op',
  required: true,
  default: 'contains',
  options: [
    { value: 'contains', labelKey: 'scripts.option.contains' },
    { value: 'equals', labelKey: 'scripts.option.equals' },
  ],
}
const textCond = (id: string) => ({
  id,
  labelKey: `scripts.cond.${id}`,
  descKey: '',
  category: 'mail',
  params: [messageParam, opParam, { name: 'value', type: 'string' as const, labelKey: 'scripts.param.value', required: true }],
  lua: `text.test({{message}}.x, {{op}}, {{value}})`,
})

/** A registry with block conditions, actions and a value type, for the block editor tests. */
export const blockCatalog: ScriptCatalog = {
  ...catalogFixture,
  triggers: [
    { id: 'manual', labelKey: 'scripts.trigger.manual', descKey: '', category: 'manual', params: [], event: [] },
    { id: 'mail.arrived', labelKey: 'scripts.trigger.mail.arrived', descKey: '', category: 'mail', params: [], event: [{ name: 'message', type: 'message', labelKey: '', label: 'Message' }], permission: 'mail.read' },
    { id: 'mail.before_send', labelKey: 'scripts.trigger.mail.before_send', descKey: '', category: 'mail', params: [], event: [] },
  ],
  conditions: [textCond('mail.from'), textCond('mail.subject'), textCond('mail.to'), { ...textCond('mail.header'), params: [messageParam, { name: 'name', type: 'string', labelKey: 'scripts.param.name', required: true }, opParam, { name: 'value', type: 'string', labelKey: 'scripts.param.value', required: true }] }],
  actions: [
    { id: 'mail.move', labelKey: 'scripts.action.mail.move', descKey: '', category: 'mail', permission: 'mail.modify', params: [messageParam, { name: 'folder', type: 'folder', labelKey: 'scripts.param.folder', required: true }], lua: 'mail.move({{message}}, {{folder}})' },
    { id: 'mail.create_draft', labelKey: 'scripts.action.mail.create_draft', descKey: '', category: 'mail', permission: 'mail.compose', params: [{ name: 'subject', type: 'string', labelKey: 'scripts.param.subject' }], outputs: [{ name: 'id', type: 'number', labelKey: 'scripts.field.id', label: 'id' }], lua: 'mail.draft({subject = {{subject}}})' },
    { id: 'notify.toast', labelKey: 'scripts.action.notify.toast', descKey: '', category: 'app', params: [{ name: 'text', type: 'string', labelKey: 'scripts.param.text', required: true }], lua: 'notify.toast({{text}})' },
    { id: 'text.check', labelKey: '', label: 'Check text', descKey: '', category: 'utility', params: [{ name: 'pattern', type: 'regex', labelKey: 'scripts.param.pattern', required: true }], lua: 'text.matches("", {{pattern}})' },
    { id: 'outgoing.cancel', labelKey: 'scripts.action.outgoing.cancel', descKey: '', category: 'mail', params: [{ name: 'reason', type: 'string', labelKey: 'scripts.param.reason' }], lua: 'outgoing.cancel({{reason}})', triggers: ['mail.before_send'] },
  ],
  valueTypes: [
    {
      id: 'message',
      labelKey: 'scripts.type.message',
      descKey: '',
      category: 'mail',
      fields: [
        { name: 'subject', type: 'string', labelKey: 'scripts.field.subject' },
        { name: 'from', type: 'address', labelKey: 'scripts.field.from' },
        { name: 'recipients', type: 'list', of: 'address', labelKey: '', label: 'recipients' },
      ],
      conditions: [
        { field: 'from', condition: 'mail.from', param: 'value', labelKey: 'scripts.blocks.rule.from' },
        { field: 'subject', condition: 'mail.subject', param: 'value', labelKey: 'scripts.blocks.rule.subject' },
        { field: 'headers.list-id', condition: 'mail.header', param: 'value', args: { name: 'List-Id' }, labelKey: 'scripts.blocks.rule.list' },
        { field: 'to', condition: 'mail.to', param: 'value', split: ',', labelKey: 'scripts.blocks.rule.to' },
      ],
    },
  ],
  permissions: [
    ...catalogFixture.permissions,
    { id: 'mail.modify', labelKey: 'scripts.perm.mail.modify', descKey: '', category: 'mail', risk: 'medium' },
    { id: 'mail.compose', labelKey: 'scripts.perm.mail.compose', descKey: '', category: 'mail', risk: 'low' },
  ],
}

/** A switched off Lua mail rule, with the given fields replaced. */
export function scriptFixture(over: Partial<Script> = {}): Script {
  return {
    id: 1,
    name: 'Archive invoices',
    description: '',
    kind: 'lua',
    source: 'mail.archive(event.message)',
    blocks: '',
    trigger: { type: 'mail.arrived', params: {} },
    permissions: ['mail.read', 'mail.modify'],
    approved: [],
    enabled: false,
    origin: 'user',
    settingsSchema: [],
    settings: {},
    catchUp: 'live',
    catchUpCap: 50,
    allowUnasked: [],
    placements: [],
    createdAt: '',
    updatedAt: '',
    lastRunAt: '',
    ...over,
  }
}
