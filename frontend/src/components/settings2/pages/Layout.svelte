<script lang="ts">
  // the Layout page: how the mail panes are arranged, where the space switcher
  // sits, whether the window uses its own title bar, the menu bar, the status
  // bar and the sidebar.
  import { createEventDispatcher } from 'svelte'
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import ChoiceCards from '../ui/ChoiceCards.svelte'
  import Segmented from '../ui/Segmented.svelte'
  import ShellSkeleton from '../skeletons/ShellSkeleton.svelte'
  import ListPreview from '../skeletons/ListPreview.svelte'
  import PaneLockPreview from '../skeletons/PaneLockPreview.svelte'
  import TitleBarPreview from '../skeletons/TitleBarPreview.svelte'
  import MenuBarPreview from '../skeletons/MenuBarPreview.svelte'
  import StatusBarPreview from '../skeletons/StatusBarPreview.svelte'
  import SidebarPreview from '../skeletons/SidebarPreview.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import SpaceNavEditor from '../SpaceNavEditor.svelte'
  import { resetSpaceNav } from '../../../lib/spaces'
  import {
    prefs,
    setLayoutMode,
    setNavStyle,
    setRailSide,
    setPaneLocked,
    setNativeTitlebar,
    setMenuBarInApp,
    setMenuBarNativeMinimal,
    setMenuBarIcons,
    setStatusClock,
    setStatusUnread,
    setSyncProgressBar,
    setSidebarIndentGuides,
    setShowAccountEmail,
    setShowFlaggedCount,
    setShowOfflineIndicator,
    setShowUnsyncedFolder,
    setViewsPlacement,
  } from '../../../stores/prefs'
  import { setEditing as setMenuBarEditing, menuBarNewItems, setNewItemsMode, type NewItemsMode } from '../../../stores/menubar'
  import { t, isMac } from '../../../lib/i18n'
  import type { LayoutMode, NavStyle, RailSide, ViewsPlacement } from '../../../lib/types'

  const dispatch = createEventDispatcher<{ close: void }>()

  $: classic = $prefs.design === 'classic'
  $: navDefault = $prefs.spaceOrder === '' && $prefs.hiddenSpaces === ''

  // the placement under the pointer in the views control, which the sidebar
  // picture shows in place of the saved one.
  let trialViews: string | null = null

  $: newItemsOptions = [
    { key: 'visible', label: $t('menuBar.newItems.visible') },
    { key: 'hidden', label: $t('menuBar.newItems.hidden') },
  ]

  $: viewsPlacementOptions = [
    { key: 'hidden', label: $t('views.setting.hidden') },
    { key: 'sidebar', label: $t('views.setting.sidebar') },
    { key: 'tab', label: $t('views.setting.tab') },
  ]

  function editMenuBar(): void {
    setMenuBarEditing(true)
    dispatch('close')
  }

  $: layouts = [
    { key: 'columns', label: $t('layout.columns'), hint: $t('layout.columns.hint') },
    { key: 'stacked', label: $t('layout.stacked'), hint: $t('layout.stacked.hint') },
  ]

  $: navs = [
    { key: 'rail', label: $t('nav.rail'), hint: $t('nav.rail.hint') },
    { key: 'footer', label: $t('nav.footer'), hint: $t('nav.footer.hint') },
    { key: 'tabs', label: $t('nav.tabs'), hint: $t('nav.tabs.hint') },
    { key: 'hidden', label: $t('nav.hidden'), hint: $t('nav.hidden.hint') },
  ]

  $: sides = [
    { key: 'left', label: $t('nav.side.left') },
    { key: 'right', label: $t('nav.side.right') },
  ]
</script>

{#if classic}
  <p class="note">{$t('layout.classicNote')}</p>
{/if}

<Card id="layout" title={$t('layout.label')} description={$t('layout.description')} bare>
  <ChoiceCards
    label={$t('layout.label')}
    value={$prefs.layoutMode}
    options={layouts}
    minWidth={220}
    disabled={classic}
    on:change={(e) => setLayoutMode(e.detail as LayoutMode)}
    let:option
  >
    <ShellSkeleton
      layout={option.key as LayoutMode}
      nav={$prefs.navStyle}
      railSide={$prefs.railSide}
      panes={$prefs.paneStyle}
      height={120}
    />
  </ChoiceCards>
</Card>

<Card id="nav" title={$t('nav.label')} description={$t('nav.description')} bare>
  <ChoiceCards
    label={$t('nav.label')}
    value={$prefs.navStyle}
    options={navs}
    disabled={classic}
    on:change={(e) => setNavStyle(e.detail as NavStyle)}
    let:option
  >
    <ShellSkeleton
      layout={$prefs.layoutMode}
      nav={option.key as NavStyle}
      railSide={$prefs.railSide}
      panes={$prefs.paneStyle}
    />
  </ChoiceCards>
</Card>

{#if $prefs.navStyle === 'rail'}
  <Card id="rail-side" title={$t('nav.side.label')} bare>
    <ChoiceCards
      label={$t('nav.side.label')}
      value={$prefs.railSide}
      options={sides}
      disabled={classic}
      on:change={(e) => setRailSide(e.detail as RailSide)}
      let:option
    >
      <ShellSkeleton
        layout={$prefs.layoutMode}
        nav="rail"
        railSide={option.key as RailSide}
        panes={$prefs.paneStyle}
      />
    </ChoiceCards>
  </Card>
{/if}

<Card id="spaces" title={$t('spaces.settings.title')} description={$t('spaces.settings.description')}>
  <SpaceNavEditor />
  <Row id="spaces-reset" label={$t('spaces.settings.reset')} hint={$t('spaces.settings.resetHint')} disabled={classic}>
    <button type="button" class="action" disabled={navDefault} on:click={resetSpaceNav}>{$t('spaces.settings.resetButton')}</button>
  </Row>
</Card>

<Card title={$t('layout.window')}>
  <Row id="pane-lock" label={$t('settings.lockPanes')} hint={$t('settingsPanel.hint.lockPanes')}>
    <PaneLockPreview slot="preview" locked={$prefs.paneLocked} />
    <ToggleSwitch checked={$prefs.paneLocked} label={$t('settings.lockPanes')} on:change={(e) => setPaneLocked(e.detail)} />
  </Row>
  <Row id="native-titlebar" label={$t('layout.nativeTitlebar')} hint={$t('layout.nativeTitlebar.hint')} disabled={classic}>
    <TitleBarPreview slot="preview" native={$prefs.nativeTitlebar} />
    <ToggleSwitch checked={$prefs.nativeTitlebar} label={$t('layout.nativeTitlebar')} on:change={(e) => setNativeTitlebar(e.detail)} />
  </Row>
</Card>

<Card title={$t('settingsPanel.category.menubar')}>
  {#if isMac}
    <Row id="menubar-in-app" label={$t('settingsPanel.toggle.menuBarInApp')} hint={$t('settingsPanel.hint.menuBarInApp')}>
      <MenuBarPreview slot="preview" inApp={$prefs.menuBarInApp} icons={$prefs.menuBarIcons} />
      <ToggleSwitch
        checked={$prefs.menuBarInApp}
        label={$t('settingsPanel.toggle.menuBarInApp')}
        on:change={(e) => setMenuBarInApp(e.detail)}
      />
    </Row>
    {#if $prefs.menuBarInApp}
      <Row id="menubar-native-minimal" label={$t('settingsPanel.toggle.menuBarNativeMinimal')} hint={$t('settingsPanel.hint.menuBarNativeMinimal')}>
        <ToggleSwitch
          checked={$prefs.menuBarNativeMinimal}
          label={$t('settingsPanel.toggle.menuBarNativeMinimal')}
          on:change={(e) => setMenuBarNativeMinimal(e.detail)}
        />
      </Row>
    {/if}
  {/if}
  {#if !isMac || $prefs.menuBarInApp}
    <Row id="menubar-icons" label={$t('settingsPanel.toggle.menuBarIcons')} hint={$t('settingsPanel.hint.menuBarIcons')}>
      <MenuBarPreview slot="preview" icons={$prefs.menuBarIcons} />
      <ToggleSwitch
        checked={$prefs.menuBarIcons}
        label={$t('settingsPanel.toggle.menuBarIcons')}
        on:change={(e) => setMenuBarIcons(e.detail)}
      />
    </Row>
    <Row id="menubar-edit" label={$t('menuBar.editButton')} hint={$t('menuBar.editHint')}>
      <button type="button" class="action" on:click={editMenuBar}>{$t('menuBar.editButton')}</button>
    </Row>
    <Row id="menubar-new-items" label={$t('menuBar.newItems.label')} hint={$t('menuBar.newItems.hint')}>
      <Segmented
        label={$t('menuBar.newItems.label')}
        value={$menuBarNewItems}
        options={newItemsOptions}
        on:change={(e) => setNewItemsMode(e.detail as NewItemsMode)}
      />
    </Row>
  {/if}
</Card>

<Card title={$t('settingsPanel.heading.statusBar')}>
  <Row id="status-clock" label={$t('settingsPanel.toggle.statusClock')}>
    <StatusBarPreview slot="preview" clock={$prefs.statusClock} unread={$prefs.statusUnread} />
    <ToggleSwitch checked={$prefs.statusClock} label={$t('settingsPanel.toggle.statusClock')} on:change={(e) => setStatusClock(e.detail)} />
  </Row>
  <Row id="status-unread" label={$t('settingsPanel.toggle.statusUnread')} hint={$t('settingsPanel.hint.statusUnread')}>
    <StatusBarPreview slot="preview" clock={$prefs.statusClock} unread={$prefs.statusUnread} />
    <ToggleSwitch checked={$prefs.statusUnread} label={$t('settingsPanel.toggle.statusUnread')} on:change={(e) => setStatusUnread(e.detail)} />
  </Row>
  <Row id="sync-progress-bar" label={$t('settingsPanel.toggle.syncProgressBar')} hint={$t('settingsPanel.hint.syncProgressBarDetail')}>
    <StatusBarPreview
      slot="preview"
      syncing
      progress={$prefs.syncProgressBar}
      clock={$prefs.statusClock}
      unread={$prefs.statusUnread}
    />
    <span title={$t('settingsPanel.hint.syncProgressBar')}>
      <ToggleSwitch
        checked={$prefs.syncProgressBar}
        label={$t('settingsPanel.toggle.syncProgressBar')}
        on:change={(e) => setSyncProgressBar(e.detail)}
      />
    </span>
  </Row>
</Card>

<Card title={$t('settingsPanel.category.sidebar')}>
  <Row id="indent-guides" label={$t('settingsPanel.toggle.indentGuides')} hint={$t('settingsPanel.hint.indentGuides')}>
    <SidebarPreview
      slot="preview"
      indentGuides={$prefs.sidebarIndentGuides}
      accountEmail={$prefs.showAccountEmail}
      flaggedCount={$prefs.showFlaggedCount}
      unsynced={$prefs.showUnsyncedFolder}
      views={$prefs.viewsPlacement as ViewsPlacement}
    />
    <ToggleSwitch
      checked={$prefs.sidebarIndentGuides}
      label={$t('settingsPanel.toggle.indentGuides')}
      on:change={(e) => setSidebarIndentGuides(e.detail)}
    />
  </Row>
  <Row id="account-email" label={$t('settingsPanel.toggle.showMailboxEmail')}>
    <SidebarPreview
      slot="preview"
      indentGuides={$prefs.sidebarIndentGuides}
      accountEmail={$prefs.showAccountEmail}
      flaggedCount={$prefs.showFlaggedCount}
      unsynced={$prefs.showUnsyncedFolder}
      views={$prefs.viewsPlacement as ViewsPlacement}
    />
    <ToggleSwitch
      checked={$prefs.showAccountEmail}
      label={$t('settingsPanel.toggle.showMailboxEmail')}
      on:change={(e) => setShowAccountEmail(e.detail)}
    />
  </Row>
  <Row id="flagged-count" label={$t('settingsPanel.toggle.flaggedCount')} hint={$t('settingsPanel.hint.flaggedCount')}>
    <SidebarPreview
      slot="preview"
      indentGuides={$prefs.sidebarIndentGuides}
      accountEmail={$prefs.showAccountEmail}
      flaggedCount={$prefs.showFlaggedCount}
      unsynced={$prefs.showUnsyncedFolder}
      views="sidebar"
    />
    <ToggleSwitch
      checked={$prefs.showFlaggedCount}
      label={$t('settingsPanel.toggle.flaggedCount')}
      on:change={(e) => setShowFlaggedCount(e.detail)}
    />
  </Row>
  <Row id="offline-indicator" label={$t('settingsPanel.toggle.offlineIndicator')} hint={$t('settingsPanel.hint.offlineIndicator')}>
    <ListPreview slot="preview" />
    <ToggleSwitch
      checked={$prefs.showOfflineIndicator}
      label={$t('settingsPanel.toggle.offlineIndicator')}
      on:change={(e) => setShowOfflineIndicator(e.detail)}
    />
  </Row>
  <Row id="unsynced-folder" label={$t('settingsPanel.toggle.unsyncedFolder')} hint={$t('settingsPanel.hint.unsyncedFolder')}>
    <SidebarPreview
      slot="preview"
      indentGuides={$prefs.sidebarIndentGuides}
      accountEmail={$prefs.showAccountEmail}
      flaggedCount={$prefs.showFlaggedCount}
      unsynced={$prefs.showUnsyncedFolder}
      views={$prefs.viewsPlacement as ViewsPlacement}
    />
    <ToggleSwitch
      checked={$prefs.showUnsyncedFolder}
      label={$t('settingsPanel.toggle.unsyncedFolder')}
      on:change={(e) => setShowUnsyncedFolder(e.detail)}
    />
  </Row>
  <Row id="views-placement" label={$t('views.setting.label')} hint={$t('views.setting.hint')}>
    <SidebarPreview
      slot="preview"
      indentGuides={$prefs.sidebarIndentGuides}
      accountEmail={$prefs.showAccountEmail}
      flaggedCount={$prefs.showFlaggedCount}
      unsynced={$prefs.showUnsyncedFolder}
      views={(trialViews ?? $prefs.viewsPlacement) as ViewsPlacement}
    />
    <Segmented
      label={$t('views.setting.label')}
      value={$prefs.viewsPlacement}
      options={viewsPlacementOptions}
      on:hover={(e) => (trialViews = e.detail)}
      on:change={(e) => setViewsPlacement(e.detail as ViewsPlacement)}
    />
  </Row>
</Card>

<style>
  .action {
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

  .action:hover:not(:disabled) {
    background: var(--surface-hover);
  }

  .action:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .note {
    margin: 0 0 var(--space-5);
    padding: var(--space-3) var(--space-4);
    border-inline-start: 2px solid var(--warning);
    background: var(--warning-bg);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }
</style>
