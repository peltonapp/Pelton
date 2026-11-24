<script lang="ts">
  // the mail layout picked by picture: three columns or stacked. the pick
  // applies to the whole app at once.
  import ChoiceCards from '../settings2/ui/ChoiceCards.svelte'
  import ShellSkeleton from '../settings2/skeletons/ShellSkeleton.svelte'
  import { prefs, setLayoutMode } from '../../stores/prefs'
  import { t } from '../../lib/i18n'
  import type { LayoutMode } from '../../lib/types'

  $: layouts = [
    { key: 'columns', label: $t('layout.columns'), hint: $t('layout.columns.hint') },
    { key: 'stacked', label: $t('layout.stacked'), hint: $t('layout.stacked.hint') },
  ]
</script>

<ChoiceCards
  label={$t('layout.label')}
  value={$prefs.layoutMode}
  options={layouts}
  minWidth={180}
  on:change={(e) => setLayoutMode(e.detail as LayoutMode)}
  let:option
>
  <ShellSkeleton
    layout={option.key as LayoutMode}
    nav={$prefs.navStyle}
    railSide={$prefs.railSide}
    panes={$prefs.paneStyle}
    height={88}
  />
</ChoiceCards>
