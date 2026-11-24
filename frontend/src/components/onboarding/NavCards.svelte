<script lang="ts">
  // where the space switcher sits, picked by picture, and for the rail which
  // side it takes. the picks apply to the whole app at once.
  import ChoiceCards from '../settings2/ui/ChoiceCards.svelte'
  import ShellSkeleton from '../settings2/skeletons/ShellSkeleton.svelte'
  import { prefs, setNavStyle, setRailSide } from '../../stores/prefs'
  import { t } from '../../lib/i18n'
  import type { NavStyle, RailSide } from '../../lib/types'

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

<ChoiceCards
  label={$t('nav.label')}
  value={$prefs.navStyle}
  options={navs}
  minWidth={150}
  on:change={(e) => setNavStyle(e.detail as NavStyle)}
  let:option
>
  <ShellSkeleton
    layout={$prefs.layoutMode}
    nav={option.key as NavStyle}
    railSide={$prefs.railSide}
    panes={$prefs.paneStyle}
    height={72}
  />
</ChoiceCards>
{#if $prefs.navStyle === 'rail'}
  <h3 class="label">{$t('nav.side.label')}</h3>
  <ChoiceCards
    label={$t('nav.side.label')}
    value={$prefs.railSide}
    options={sides}
    minWidth={150}
    on:change={(e) => setRailSide(e.detail as RailSide)}
    let:option
  >
    <ShellSkeleton
      layout={$prefs.layoutMode}
      nav="rail"
      railSide={option.key as RailSide}
      panes={$prefs.paneStyle}
      height={56}
    />
  </ChoiceCards>
{/if}

<style>
  .label {
    margin: var(--space-6) 0 var(--space-3);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }
</style>
