<script lang="ts">
  // the General page: settings about the app as a whole. language, what happens
  // at launch and on close, how times read, update checks and shortcut hints.
  import folderIcon from '@phosphor-icons/core/assets/duotone/folder-open-duotone.svg?raw'
  import templateIcon from '@phosphor-icons/core/assets/duotone/file-arrow-down-duotone.svg?raw'
  import reloadIcon from '@phosphor-icons/core/assets/duotone/arrow-clockwise-duotone.svg?raw'
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import Segmented from '../ui/Segmented.svelte'
  import Ph from '../../common/Ph.svelte'
  import Select from '../../common/Select.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import LanguageSelect from '../../common/LanguageSelect.svelte'
  import type { SelectItem } from '../../../lib/selectnav'
  import {
    prefs,
    setLanguage,
    setTimeFormat,
    setStartupSelection,
    setRestoreTabs,
    setCloseAction,
    setStartHidden,
  } from '../../../stores/prefs'
  import { sidebar } from '../../../stores/accounts'
  import { openLocalesFolder, saveLocaleTemplate } from '../../../lib/api'
  import en from '../../../lib/locales/en'
  import { toastInfo, toastError, errorMessage } from '../../../stores/toast'
  import { t, isMac } from '../../../lib/i18n'
  import type { CloseAction } from '../../../lib/types'

  // remounts the picker so newly dropped language files show up without
  // leaving settings.
  let languageReload = 0

  async function onOpenLocalesFolder(): Promise<void> {
    try {
      await openLocalesFolder()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // writes a ready-to-translate template: every English string plus the meta
  // fields a language file needs.
  async function onSaveLocaleTemplate(): Promise<void> {
    const template = JSON.stringify({ name: 'My Language', author: '', base: 'en', strings: en }, null, 2)
    try {
      const path = await saveLocaleTemplate(template)
      if (path) {
        toastInfo($t('language.templateSaved'))
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // unified view names are localized by key in the sidebar; reuse that here so
  // the setting and the row it points at read the same.
  function unifiedViewName(key: string, fallback: string): string {
    const lookup = key === 'inbox' ? 'sidebar.unifiedInbox' : `sidebar.view.${key}`
    const translated = $t(lookup)
    return translated === lookup ? fallback : translated
  }

  // "last used", then every unified view and every folder grouped by account.
  $: startupItems = [
    { value: 'last', label: $t('settingsPanel.startup.lastUsed') },
    ...($sidebar.data
      ? [
          {
            label: $t('sidebar.unifiedViews.heading'),
            options: $sidebar.data.views.map((view) => ({
              value: `view:${view.key}`,
              label: unifiedViewName(view.key, view.label),
            })),
          },
          ...$sidebar.data.accounts.map((account) => ({
            label: account.email,
            options: ($sidebar.data?.foldersByAccount[account.id] ?? []).map((folder) => ({
              value: `folder:${folder.id}`,
              label: folder.imapPath,
            })),
          })),
        ]
      : []),
  ] satisfies SelectItem[]

  $: closeActionOptions = [
    { key: 'background', label: $t('settingsPanel.closeAction.background') },
    { key: 'quit', label: $t('settingsPanel.closeAction.quit') },
  ]

  $: timeFormatOptions = [
    { key: 'auto', label: $t('settingsPanel.timeFormat.auto') },
    { key: '12', label: $t('settingsPanel.timeFormat.h12') },
    { key: '24', label: $t('settingsPanel.timeFormat.h24') },
  ]
</script>

<Card title={$t('settings2.general.region')}>
  <Row id="language" label={$t('settings.language')} hint={$t('settings.languageHint')} stacked>
    {#key languageReload}
      <LanguageSelect value={$prefs.language} onSelect={setLanguage} />
    {/key}
  </Row>
  <Row id="language-custom" label={$t('language.custom')} hint={$t('language.customHint')} stacked>
    <div class="tools">
      <button type="button" class="tool" title={$t('language.openFolderHint')} on:click={onOpenLocalesFolder}>
        <Ph svg={folderIcon} size={15} />
        {$t('language.openFolder')}
      </button>
      <button type="button" class="tool" title={$t('language.templateHint')} on:click={onSaveLocaleTemplate}>
        <Ph svg={templateIcon} size={15} />
        {$t('language.template')}
      </button>
      <button type="button" class="tool" title={$t('language.reloadHint')} on:click={() => (languageReload += 1)}>
        <Ph svg={reloadIcon} size={15} />
        {$t('language.reload')}
      </button>
    </div>
  </Row>
  <Row id="time-format" label={$t('settingsPanel.label.timeFormat')} hint={$t('settingsPanel.hint.timeFormat')}>
    <Segmented
      label={$t('settingsPanel.label.timeFormat')}
      value={$prefs.timeFormat}
      options={timeFormatOptions}
      on:change={(e) => setTimeFormat(e.detail)}
    />
  </Row>
</Card>

<Card title={$t('settings2.general.startup')}>
  <Row id="startup-selection" label={$t('settingsPanel.label.startupSelection')} hint={$t('settingsPanel.hint.startupSelection')}>
    <Select
      value={$prefs.startupSelection}
      ariaLabel={$t('settingsPanel.label.startupSelection')}
      items={startupItems}
      on:change={(e) => setStartupSelection(e.detail)}
    />
  </Row>
  <Row id="restore-tabs" label={$t('settingsPanel.toggle.restoreTabs')} hint={$t('settingsPanel.hint.restoreTabs')}>
    <ToggleSwitch checked={$prefs.restoreTabs} label={$t('settingsPanel.toggle.restoreTabs')} on:change={(e) => setRestoreTabs(e.detail)} />
  </Row>
  <Row
    id="start-hidden"
    label={$t('settingsPanel.toggle.startHidden')}
    hint={isMac ? $t('settingsPanel.hint.startHiddenMac') : $t('settingsPanel.hint.startHidden')}
  >
    <ToggleSwitch checked={$prefs.startHidden} label={$t('settingsPanel.toggle.startHidden')} on:change={(e) => setStartHidden(e.detail)} />
  </Row>
  <Row id="close-action" label={$t('settingsPanel.label.closeAction')} hint={$t('settingsPanel.hint.closeAction')}>
    <Segmented
      label={$t('settingsPanel.label.closeAction')}
      value={$prefs.closeAction}
      options={closeActionOptions}
      on:change={(e) => setCloseAction(e.detail as CloseAction)}
    />
  </Row>
</Card>


<style>
  .tools {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  .tool {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-height);
    padding: 0 var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-size: var(--fz-label);
    color: var(--text-primary);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .tool:hover {
    background: var(--surface-hover);
  }
</style>
