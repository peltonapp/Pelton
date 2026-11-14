<script lang="ts">
  // the Layout page: how the mail panes are arranged, where the space switcher
  // sits, and whether the window uses its own title bar.
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import ChoiceCards from '../ui/ChoiceCards.svelte'
  import ShellSkeleton from '../skeletons/ShellSkeleton.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import { prefs, setLayoutMode, setNavStyle, setRailSide, setPaneLocked, setNativeTitlebar } from '../../../stores/prefs'
  import { t } from '../../../lib/i18n'
  import type { LayoutMode, NavStyle, RailSide } from '../../../lib/types'

  $: classic = $prefs.design === 'classic'

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

<Card title={$t('layout.label')} description={$t('layout.description')} bare>
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

<Card title={$t('nav.label')} description={$t('nav.description')} bare>
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
  <Card title={$t('nav.side.label')} bare>
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

<Card title={$t('layout.window')}>
  <Row id="pane-lock" label={$t('settings.lockPanes')} hint={$t('settingsPanel.hint.lockPanes')}>
    <ToggleSwitch checked={$prefs.paneLocked} label={$t('settings.lockPanes')} on:change={(e) => setPaneLocked(e.detail)} />
  </Row>
  <Row id="native-titlebar" label={$t('layout.nativeTitlebar')} hint={$t('layout.nativeTitlebar.hint')} disabled={classic}>
    <ToggleSwitch checked={$prefs.nativeTitlebar} label={$t('layout.nativeTitlebar')} on:change={(e) => setNativeTitlebar(e.detail)} />
  </Row>
</Card>

<style>
  .note {
    margin: 0 0 var(--space-5);
    padding: var(--space-3) var(--space-4);
    border-inline-start: 2px solid var(--warning);
    background: var(--warning-bg);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }
</style>
