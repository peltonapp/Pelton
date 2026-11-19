// location-automations.ts lets Automations take part in back and forward: the
// script open in the space. a deleted script's place is gone.

import { get } from 'svelte/store'
import { registerLocationProvider, oneSideEmpty, type AutomationsLocation } from './locations'
import { scripts, selectedScriptId, selectScript } from '../stores/scripts'
import { openSpace } from '../stores/space'

registerLocationProvider('automations', {
  watch: [selectedScriptId, scripts],
  describe(): AutomationsLocation {
    const id = get(selectedScriptId)
    return { space: 'automations', script: id ?? undefined }
  },
  label(loc) {
    return loc.script === undefined ? '' : (get(scripts).find((s) => s.id === loc.script)?.name ?? '')
  },
  async restore(loc) {
    if (loc.script !== undefined && !get(scripts).some((s) => s.id === loc.script)) {
      return false
    }
    openSpace('automations')
    selectScript(loc.script ?? null)
    return true
  },
  refines(from, to) {
    return oneSideEmpty(from.script, to.script)
  },
})
