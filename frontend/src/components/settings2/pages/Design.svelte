<script lang="ts">
  // the Design page: which interface the app wears, and how its panes are drawn.
  import Card from '../ui/Card.svelte'
  import ChoiceCards from '../ui/ChoiceCards.svelte'
  import ShellSkeleton from '../skeletons/ShellSkeleton.svelte'
  import { prefs, setDesign, setPaneStyle } from '../../../stores/prefs'
  import { t } from '../../../lib/i18n'
  import type { DesignPref, PaneStyle } from '../../../lib/types'

  $: designs = [
    { key: 'new', label: $t('design.new'), hint: $t('design.new.hint') },
    { key: 'classic', label: $t('design.classic'), hint: $t('design.classic.hint') },
  ]

  $: paneStyles = [
    { key: 'hybrid', label: $t('panes.hybrid'), hint: $t('panes.hybrid.hint') },
    { key: 'flat', label: $t('panes.flat'), hint: $t('panes.flat.hint') },
    { key: 'soft', label: $t('panes.soft'), hint: $t('panes.soft.hint') },
  ]
</script>

<Card id="design" title={$t('design.label')} description={$t('design.hint')} bare>
  <ChoiceCards
    label={$t('design.label')}
    value={$prefs.design}
    options={designs}
    minWidth={220}
    on:change={(e) => setDesign(e.detail as DesignPref)}
    let:option
  >
    <ShellSkeleton
      design={option.key as DesignPref}
      layout={$prefs.layoutMode}
      nav={$prefs.navStyle}
      railSide={$prefs.railSide}
      panes={$prefs.paneStyle}
      height={120}
    />
  </ChoiceCards>
</Card>

<Card id="panes" title={$t('panes.label')} description={$t('panes.description')} bare>
  <ChoiceCards
    label={$t('panes.label')}
    value={$prefs.paneStyle}
    options={paneStyles}
    on:change={(e) => setPaneStyle(e.detail as PaneStyle)}
    let:option
  >
    <ShellSkeleton
      layout={$prefs.layoutMode}
      nav={$prefs.navStyle}
      railSide={$prefs.railSide}
      panes={option.key as PaneStyle}
    />
  </ChoiceCards>
</Card>
