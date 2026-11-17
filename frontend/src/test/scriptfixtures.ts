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
    createdAt: '',
    updatedAt: '',
    lastRunAt: '',
    ...over,
  }
}
