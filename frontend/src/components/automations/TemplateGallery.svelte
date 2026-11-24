<script lang="ts">
  // the template gallery: the ready made scripts the registry offers, each
  // with what it does, what it starts on, the permissions it will ask for and
  // whether it sends anything to an AI provider. a preview shows its blocks
  // or its Lua; "use" makes a copy that starts switched off.
  import aiIcon from '@phosphor-icons/core/assets/duotone/sparkle-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import LuaEditor from './LuaEditor.svelte'
  import { t } from '../../lib/i18n'
  import { entryDescription, entryLabel, permissionDef, type Block, type BlockCond, type ScriptCatalog, type ScriptTemplateDef } from '../../lib/scripting'
  import { useTemplate } from '../../stores/scripts'
  import { errorMessage, toastError } from '../../stores/toast'
  import '../../theme/controls.css'

  /** The registry the templates come from. */
  export let catalog: ScriptCatalog

  let open = ''
  let busy = ''

  $: templates = catalog.templates ?? []

  function triggerName(tm: ScriptTemplateDef): string {
    const def = catalog.triggers.find((tr) => tr.id === tm.trigger.type)
    return def ? entryLabel($t, def) : tm.trigger.type
  }

  function condText(c: BlockCond): string {
    if (c.kind === 'condition') {
      const def = catalog.conditions.find((d) => d.id === c.type)
      return def ? entryLabel($t, def) : (c.type ?? '')
    }
    const inner = (c.items ?? []).map(condText).join(', ')
    return `${$t(`scripts.blocks.group.${c.kind}`)} (${inner})`
  }

  // the blocks as indented lines of text, for the preview.
  function outline(blocks: Block[], depth = 0): { depth: number; text: string }[] {
    const out: { depth: number; text: string }[] = []
    for (const b of blocks) {
      if (b.kind === 'action') {
        const def = catalog.actions.find((a) => a.id === b.type)
        out.push({ depth, text: def ? entryLabel($t, def) : (b.type ?? '') })
      } else if (b.kind === 'if') {
        ;(b.branches ?? []).forEach((br, i) => {
          out.push({ depth, text: `${$t(i === 0 ? 'scripts.gallery.if' : 'scripts.gallery.elseIf')} ${condText(br.when)}` })
          out.push(...outline(br.body, depth + 1))
        })
        if (b.else && b.else.length > 0) {
          out.push({ depth, text: $t('scripts.gallery.else') })
          out.push(...outline(b.else, depth + 1))
        }
      } else {
        out.push({ depth, text: $t(`scripts.blocks.kind.${b.kind}`) })
        out.push(...outline(b.body ?? [], depth + 1))
      }
    }
    return out
  }

  async function use(id: string): Promise<void> {
    busy = id
    try {
      await useTemplate(id)
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = ''
    }
  }
</script>

<section class="gallery" aria-label={$t('scripts.gallery.title')}>
  <header class="bar">
    <h2 class="title">{$t('scripts.gallery.title')}</h2>
  </header>
  <div class="scroll">
    <p class="hint">{$t('scripts.gallery.hint')}</p>
    {#if templates.length === 0}
      <p class="hint">{$t('scripts.gallery.empty')}</p>
    {/if}
    <ul class="cards">
      {#each templates as tm (tm.id)}
        <li class="card">
          <div class="head">
            <h3 class="name">{entryLabel($t, tm)}</h3>
            {#if tm.ai}<span class="badge ai"><Ph svg={aiIcon} size={12} />{$t('scripts.gallery.ai')}</span>{/if}
            <span class="badge">{$t(tm.kind === 'blocks' ? 'scripts.kind.blocks' : 'scripts.gallery.lua')}</span>
          </div>
          <p class="desc">{entryDescription($t, tm)}</p>
          <p class="meta">{$t('scripts.gallery.runs').replace('{trigger}', triggerName(tm))}</p>
          {#if tm.permissions.length > 0}
            <div class="perms" aria-label={$t('scripts.gallery.needs')}>
              <span class="meta">{$t('scripts.gallery.needs')}</span>
              {#each tm.permissions as p (p)}
                {@const def = permissionDef(catalog, p)}
                <span class="perm risk-{def?.risk ?? 'low'}" title={def ? entryDescription($t, def) : p}>{def ? entryLabel($t, def) : p}</span>
              {/each}
            </div>
          {/if}
          {#if tm.missing}<p class="missing">{$t('scripts.gallery.missing').replace('{reason}', tm.missing)}</p>{/if}
          <div class="row">
            <button type="button" class="cal-btn" aria-expanded={open === tm.id} on:click={() => (open = open === tm.id ? '' : tm.id)}>
              {$t(open === tm.id ? 'scripts.gallery.hidePreview' : 'scripts.gallery.preview')}
            </button>
            <button type="button" class="cal-btn primary" disabled={busy !== ''} on:click={() => void use(tm.id)}>{$t('scripts.gallery.use')}</button>
          </div>
          {#if open === tm.id}
            {#if tm.kind === 'blocks' && tm.blocks}
              <ol class="outline" aria-label={$t('scripts.gallery.preview')}>
                {#each outline(tm.blocks.blocks) as line, i (i)}
                  <li style={`--depth:${line.depth}`}>{line.text}</li>
                {/each}
              </ol>
            {:else}
              <div class="source">
                <LuaEditor readonly label={$t('scripts.gallery.preview')} content={tm.source ?? ''} />
              </div>
            {/if}
          {/if}
        </li>
      {/each}
    </ul>
  </div>
</section>

<style>
  .gallery {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    background: var(--surface-base);
  }

  .bar {
    display: flex;
    align-items: center;
    min-height: calc(var(--control-height) + 2 * var(--space-3));
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .title {
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--pane-pad);
  }

  .hint {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .cards {
    margin: 0;
    padding: 0;
    list-style: none;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(calc(var(--popover-width) * 1.2), 1fr));
    gap: var(--space-3);
  }

  .card {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }

  .name {
    flex: 1;
    min-width: 0;
    margin: 0;
    font-size: var(--fz-list);
    font-weight: var(--fw-semibold);
  }

  .badge {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: 0 var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
    line-height: var(--chip-height);
    color: var(--text-tertiary);
  }

  .badge.ai {
    border-color: var(--accent);
    color: var(--accent);
  }

  .desc {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .meta {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .perms {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1);
  }

  .perm {
    padding: 0 var(--space-1);
    border: var(--hairline) solid currentColor;
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
    line-height: var(--chip-height);
    color: var(--text-tertiary);
  }

  .perm.risk-medium {
    color: var(--warning);
  }

  .perm.risk-high {
    color: var(--danger);
  }

  .missing {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--warning);
  }

  .row {
    display: flex;
    gap: var(--space-2);
    margin-top: auto;
  }

  .outline {
    margin: 0;
    padding: var(--space-2);
    list-style: none;
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .outline li {
    padding-inline-start: calc(var(--depth) * var(--space-4));
  }

  .source {
    display: flex;
    min-height: calc(var(--control-height) * 8);
  }
</style>
