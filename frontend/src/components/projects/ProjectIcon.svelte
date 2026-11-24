<script lang="ts">
  // a project's icon. a tabler icon draws from the geometry the project
  // stores, so the tabler dataset is never loaded to show it; a name without
  // geometry is one of the older phosphor icons, and anything else draws the
  // default. it paints with currentColor, so the caller colors it.
  import MenuGlyph from '../common/MenuGlyph.svelte'
  import Ph from '../common/Ph.svelte'
  import { parseIconNodes, projectIcon } from '../../lib/projecticons'
  import type { IconNode } from '../../lib/menuactions'

  /** The icon name the project stores, empty for the default. */
  export let icon = ''
  /** The geometry of a tabler icon, as the json the project stores or parsed nodes. */
  export let iconNodes: string | IconNode[] | undefined = undefined
  /** Box edge in px, the size of a phosphor icon beside it. */
  export let size = 16

  // the outline glyph has a wider margin in its 24 unit box than a phosphor
  // glyph has in its own, so it is drawn a little larger to look the same size.
  const outlineScale = 1.12
  const outlineStroke = 1.7

  $: nodes = typeof iconNodes === 'string' ? parseIconNodes(iconNodes) : iconNodes?.length ? iconNodes : undefined
</script>

{#if nodes}
  <span class="outline" style={`width:${size}px;height:${size}px`} data-testid="project-icon" aria-hidden="true">
    <MenuGlyph iconNodes={nodes} size={Math.round(size * outlineScale)} stroke={outlineStroke} />
  </span>
{:else}
  <Ph svg={projectIcon(icon)} {size} />
{/if}

<style>
  .outline {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    line-height: 0;
  }
</style>
