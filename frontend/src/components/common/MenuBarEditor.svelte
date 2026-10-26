<script lang="ts">
  // MenuBarEditor is the in-place editor for the menu bar, shown in the bar's
  // slot while editing mode is on. The chip row is the bar itself: drag to
  // reorder menus, + to add a top-level menu. Selecting a menu opens its panel
  // below where its items live: drag to reorder, + to add an item, submenu or
  // separator, and click a custom item or submenu to edit its label, action and
  // icon inline. Everything writes straight to stores/menubar.ts, so the change
  // persists and (on Done) the normal bar shows it. Submenus are one level deep.
  import { flip } from 'svelte/animate'
  import { dndzone, type DndEvent } from 'svelte-dnd-action'
  import {
    IconGripVertical,
    IconEye,
    IconEyeOff,
    IconTrash,
    IconPlus,
    IconPencil,
    IconChevronRight,
    IconChevronDown,
    IconRotate,
    IconSeparatorHorizontal,
    IconPhoto,
    IconFolderPlus,
    IconCheck,
  } from '@tabler/icons-svelte'
  import ThemedIcon from './ThemedIcon.svelte'
  import Select from './Select.svelte'
  import MenuGlyph from './MenuGlyph.svelte'
  import IconPicker from '../settings/IconPicker.svelte'
  import { t, isMac } from '../../lib/i18n'
  import { prefs } from '../../stores/prefs'
  import {
    menuActionCatalog,
    catalogByAction,
    type MenuLayout,
    type MenuItemLayout,
    type MenuActionId,
    type IconNode,
  } from '../../lib/menuactions'
  import {
    menuBarLayout,
    setEditing,
    setMenus,
    toggleMenuHidden,
    toggleItemHidden,
    addCustomItem,
    addSeparator,
    addSubmenu,
    removeItem,
    addCustomMenu,
    renameCustomMenu,
    removeMenu,
    updateItem,
    resetLayout,
  } from '../../stores/menubar'

  // local mirror the drag events mutate; resyncs whenever the store changes.
  let menuList: MenuLayout[] = $menuBarLayout.menus
  $: menuList = $menuBarLayout.menus

  $: flipMs = $prefs.reduceMotion ? 0 : 150
  $: actionOptions = menuActionCatalog.filter((d) => !(d.macOnly && !isMac))
  $: actionItems = actionOptions.map((opt) => ({ value: opt.action as string, label: $t(opt.labelKey) }))

  let openMenu: string | null = menuList[0]?.id ?? null
  $: if (openMenu !== null && !menuList.some((m) => m.id === openMenu)) {
    openMenu = menuList[0]?.id ?? null
  }

  let expandedSubs = new Set<string>()
  let editingId: string | null = null
  let pickingIcon = false
  let confirmingReset = false

  function selectMenu(id: string): void {
    openMenu = id
    editingId = null
    pickingIcon = false
  }
  function toggleSub(id: string): void {
    const next = new Set(expandedSubs)
    next.has(id) ? next.delete(id) : next.add(id)
    expandedSubs = next
  }
  function startEdit(id: string): void {
    editingId = editingId === id ? null : id
    pickingIcon = false
  }

  // --- drag and drop ---

  function setLocalItems(menuId: string, subId: string | null, items: MenuItemLayout[]): void {
    menuList = menuList.map((m) => {
      if (m.id !== menuId) {
        return m
      }
      if (subId === null) {
        return { ...m, items }
      }
      return { ...m, items: m.items.map((it) => (it.id === subId ? { ...it, items } : it)) }
    })
  }
  function menusConsider(e: CustomEvent<DndEvent<MenuLayout>>): void {
    menuList = e.detail.items
  }
  function menusFinalize(e: CustomEvent<DndEvent<MenuLayout>>): void {
    menuList = e.detail.items
    setMenus(menuList)
  }
  function itemsConsider(menuId: string, subId: string | null, e: CustomEvent<DndEvent<MenuItemLayout>>): void {
    setLocalItems(menuId, subId, e.detail.items)
  }
  function itemsFinalize(menuId: string, subId: string | null, e: CustomEvent<DndEvent<MenuItemLayout>>): void {
    setLocalItems(menuId, subId, e.detail.items)
    setMenus(menuList)
  }

  // --- labels ---

  function menuLabel(menu: MenuLayout): string {
    return menu.labelKey ? $t(menu.labelKey) : (menu.label ?? '')
  }
  function itemDef(item: MenuItemLayout) {
    return item.action ? catalogByAction[item.action] : undefined
  }
  function itemLabel(item: MenuItemLayout): string {
    if (item.kind === 'custom' || item.kind === 'submenu') {
      return item.label ?? ''
    }
    const def = itemDef(item)
    return def ? $t(def.labelKey) : ''
  }
  function isMacOnly(item: MenuItemLayout): boolean {
    return !!itemDef(item)?.macOnly && !isMac
  }
  function editable(item: MenuItemLayout): boolean {
    return item.kind === 'custom' || item.kind === 'submenu'
  }

  // --- add / edit ---

  function addMenu(): void {
    const id = addCustomMenu($t('menuBar.newMenu'))
    openMenu = id
  }
  function addItem(menuId: string, subId: string | null): void {
    const id = addCustomItem(menuId, subId, {
      label: $t('menuBar.newItem'),
      action: actionOptions[0]?.action ?? 'compose',
    })
    editingId = id
    pickingIcon = false
  }
  function addSub(menuId: string): void {
    const id = addSubmenu(menuId, $t('menuBar.newSubmenu'))
    expandedSubs = new Set(expandedSubs).add(id)
  }
  function onIconSelect(menuId: string, subId: string | null, itemId: string, e: CustomEvent<{ iconName: string; iconNodes?: IconNode[] }>): void {
    updateItem(menuId, subId, itemId, { iconName: e.detail.iconName, iconNodes: e.detail.iconNodes })
    pickingIcon = false
  }
  function onIconClear(menuId: string, subId: string | null, itemId: string): void {
    updateItem(menuId, subId, itemId, { iconName: undefined, iconNodes: undefined })
    pickingIcon = false
  }

  function doReset(): void {
    resetLayout()
    confirmingReset = false
    expandedSubs = new Set()
    editingId = null
  }
</script>

<div class="editor">
  <!-- the chip row is the bar: reorder menus, add a top-level menu -->
  <div class="bar">
    <section
      class="chips"
      use:dndzone={{ items: menuList, type: 'mb-menus', flipDurationMs: flipMs, dropTargetStyle: {} }}
      on:consider={menusConsider}
      on:finalize={menusFinalize}
    >
      {#each menuList as menu (menu.id)}
        <div class="chip" class:active={openMenu === menu.id} class:dimmed={menu.hidden} animate:flip={{ duration: flipMs }}>
          <span class="grip" aria-hidden="true"><IconGripVertical size={14} stroke={1.7} /></span>
          <button type="button" class="chip-title" on:click={() => selectMenu(menu.id)}>{menuLabel(menu)}</button>
        </div>
      {/each}
    </section>
    <button type="button" class="add-chip" title={$t('menuBar.addMenu')} on:click={addMenu}>
      <IconPlus size={15} stroke={1.7} />
    </button>
    <span class="spacer"></span>
    {#if confirmingReset}
      <button type="button" class="txt-btn" on:click={() => (confirmingReset = false)}>{$t('menuBar.cancel')}</button>
      <button type="button" class="txt-btn danger" on:click={doReset}>{$t('menuBar.reset')}</button>
    {:else}
      <button type="button" class="txt-btn" on:click={() => (confirmingReset = true)}>
        <IconRotate size={14} stroke={1.7} /><span>{$t('menuBar.reset')}</span>
      </button>
    {/if}
    <button type="button" class="done-btn" on:click={() => setEditing(false)}>
      <IconCheck size={14} stroke={1.7} /><span>{$t('menuBar.done')}</span>
    </button>
  </div>

  {#each menuList as menu (menu.id)}
    {#if openMenu === menu.id}
      <div class="panel">
        <div class="panel-head">
          {#if menu.builtin}
            <span class="menu-name">{menuLabel(menu)}</span>
          {:else}
            <input
              class="name-input"
              type="text"
              value={menu.label ?? ''}
              aria-label={$t('menuBar.menuName')}
              on:input={(e) => renameCustomMenu(menu.id, e.currentTarget.value)}
            />
          {/if}
          <span class="spacer"></span>
          <button type="button" class="icon-btn" title={menu.hidden ? $t('menuBar.show') : $t('menuBar.hide')} on:click={() => toggleMenuHidden(menu.id)}>
            {#if menu.hidden}<IconEyeOff size={16} stroke={1.7} />{:else}<IconEye size={16} stroke={1.7} />{/if}
          </button>
          {#if !menu.builtin}
            <button type="button" class="icon-btn danger" title={$t('menuBar.removeMenu')} on:click={() => removeMenu(menu.id)}>
              <IconTrash size={16} stroke={1.7} />
            </button>
          {/if}
        </div>

        <div
          class="rows"
          use:dndzone={{ items: menu.items, type: 'mb-items', flipDurationMs: flipMs, dropTargetStyle: {} }}
          on:consider={(e) => itemsConsider(menu.id, null, e)}
          on:finalize={(e) => itemsFinalize(menu.id, null, e)}
        >
          {#each menu.items as item (item.id)}
            {@const def = itemDef(item)}
            <div class="row-wrap" animate:flip={{ duration: flipMs }}>
              <div class="row" class:dimmed={item.hidden}>
                <span class="grip" aria-hidden="true"><IconGripVertical size={15} stroke={1.7} /></span>

                {#if item.kind === 'separator'}
                  <span class="glyph"><IconSeparatorHorizontal size={15} stroke={1.7} /></span>
                  <span class="row-label muted">{$t('menuBar.separator')}</span>
                {:else if item.kind === 'submenu'}
                  <button type="button" class="expander" on:click={() => toggleSub(item.id)} aria-expanded={expandedSubs.has(item.id)}>
                    {#if expandedSubs.has(item.id)}<IconChevronDown size={15} stroke={1.7} />{:else}<IconChevronRight size={15} stroke={1.7} />{/if}
                  </button>
                  <span class="glyph"><MenuGlyph iconName={item.iconName} iconNodes={item.iconNodes} size={15} /></span>
                  <span class="row-label">{item.label ?? ''}</span>
                {:else}
                  <span class="glyph">
                    {#if item.kind === 'custom'}
                      <MenuGlyph iconName={item.iconName} iconNodes={item.iconNodes} size={15} />
                    {:else if def}
                      <ThemedIcon name={def.iconName} icon={def.icon} size={15} stroke={1.7} />
                    {/if}
                  </span>
                  <span class="row-label">{itemLabel(item)}</span>
                  {#if isMacOnly(item)}<span class="badge">{$t('menuBar.macOnly')}</span>{/if}
                {/if}

                <span class="spacer"></span>

                {#if editable(item)}
                  <button type="button" class="icon-btn" title={$t('menuBar.edit')} class:on={editingId === item.id} on:click={() => startEdit(item.id)}>
                    <IconPencil size={15} stroke={1.7} />
                  </button>
                {/if}
                {#if item.kind !== 'separator'}
                  <button type="button" class="icon-btn" title={item.hidden ? $t('menuBar.show') : $t('menuBar.hide')} on:click={() => toggleItemHidden(menu.id, null, item.id)}>
                    {#if item.hidden}<IconEyeOff size={15} stroke={1.7} />{:else}<IconEye size={15} stroke={1.7} />{/if}
                  </button>
                {/if}
                {#if item.kind !== 'action'}
                  <button type="button" class="icon-btn danger" title={$t('menuBar.remove')} on:click={() => removeItem(menu.id, null, item.id)}>
                    <IconTrash size={15} stroke={1.7} />
                  </button>
                {/if}
              </div>

              {#if editingId === item.id && editable(item)}
                <div class="edit-form">
                  <div class="edit-row">
                    <input
                      class="text-input"
                      type="text"
                      value={item.label ?? ''}
                      placeholder={$t('menuBar.entryLabel')}
                      aria-label={$t('menuBar.entryLabel')}
                      on:input={(e) => updateItem(menu.id, null, item.id, { label: e.currentTarget.value })}
                    />
                    {#if item.kind === 'custom'}
                      <Select
                        class="action-select"
                        value={item.action ?? ''}
                        ariaLabel={$t('menuBar.entryAction')}
                        items={actionItems}
                        on:change={(e) => updateItem(menu.id, null, item.id, { action: e.detail as MenuActionId })}
                      />
                    {/if}
                    <button type="button" class="icon-choose" on:click={() => (pickingIcon = !pickingIcon)}>
                      {#if item.iconName || item.iconNodes}
                        <MenuGlyph iconName={item.iconName} iconNodes={item.iconNodes} size={16} />
                      {:else}
                        <IconPhoto size={16} stroke={1.7} />
                      {/if}
                      <span>{$t('menuBar.icon.choose')}</span>
                    </button>
                  </div>
                  {#if pickingIcon}
                    <IconPicker
                      on:select={(e) => onIconSelect(menu.id, null, item.id, e)}
                      on:clear={() => onIconClear(menu.id, null, item.id)}
                    />
                  {/if}
                </div>
              {/if}

              {#if item.kind === 'submenu' && expandedSubs.has(item.id)}
                <div class="sub">
                  <div
                    class="rows"
                    use:dndzone={{ items: item.items ?? [], type: `mb-sub-${item.id}`, flipDurationMs: flipMs, dropTargetStyle: {} }}
                    on:consider={(e) => itemsConsider(menu.id, item.id, e)}
                    on:finalize={(e) => itemsFinalize(menu.id, item.id, e)}
                  >
                    {#each item.items ?? [] as child (child.id)}
                      {@const cdef = itemDef(child)}
                      <div class="row-wrap" animate:flip={{ duration: flipMs }}>
                        <div class="row" class:dimmed={child.hidden}>
                          <span class="grip" aria-hidden="true"><IconGripVertical size={15} stroke={1.7} /></span>
                          {#if child.kind === 'separator'}
                            <span class="glyph"><IconSeparatorHorizontal size={15} stroke={1.7} /></span>
                            <span class="row-label muted">{$t('menuBar.separator')}</span>
                          {:else}
                            <span class="glyph">
                              {#if child.kind === 'custom'}
                                <MenuGlyph iconName={child.iconName} iconNodes={child.iconNodes} size={15} />
                              {:else if cdef}
                                <ThemedIcon name={cdef.iconName} icon={cdef.icon} size={15} stroke={1.7} />
                              {/if}
                            </span>
                            <span class="row-label">{itemLabel(child)}</span>
                            {#if isMacOnly(child)}<span class="badge">{$t('menuBar.macOnly')}</span>{/if}
                          {/if}
                          <span class="spacer"></span>
                          {#if editable(child)}
                            <button type="button" class="icon-btn" title={$t('menuBar.edit')} class:on={editingId === child.id} on:click={() => startEdit(child.id)}>
                              <IconPencil size={15} stroke={1.7} />
                            </button>
                          {/if}
                          {#if child.kind !== 'separator'}
                            <button type="button" class="icon-btn" title={child.hidden ? $t('menuBar.show') : $t('menuBar.hide')} on:click={() => toggleItemHidden(menu.id, item.id, child.id)}>
                              {#if child.hidden}<IconEyeOff size={15} stroke={1.7} />{:else}<IconEye size={15} stroke={1.7} />{/if}
                            </button>
                          {/if}
                          {#if child.kind !== 'action'}
                            <button type="button" class="icon-btn danger" title={$t('menuBar.remove')} on:click={() => removeItem(menu.id, item.id, child.id)}>
                              <IconTrash size={15} stroke={1.7} />
                            </button>
                          {/if}
                        </div>

                        {#if editingId === child.id && child.kind === 'custom'}
                          <div class="edit-form">
                            <div class="edit-row">
                              <input
                                class="text-input"
                                type="text"
                                value={child.label ?? ''}
                                placeholder={$t('menuBar.entryLabel')}
                                aria-label={$t('menuBar.entryLabel')}
                                on:input={(e) => updateItem(menu.id, item.id, child.id, { label: e.currentTarget.value })}
                              />
                              <Select
                                class="action-select"
                                value={child.action ?? ''}
                                ariaLabel={$t('menuBar.entryAction')}
                                items={actionItems}
                                on:change={(e) => updateItem(menu.id, item.id, child.id, { action: e.detail as MenuActionId })}
                              />
                              <button type="button" class="icon-choose" on:click={() => (pickingIcon = !pickingIcon)}>
                                {#if child.iconName || child.iconNodes}
                                  <MenuGlyph iconName={child.iconName} iconNodes={child.iconNodes} size={16} />
                                {:else}
                                  <IconPhoto size={16} stroke={1.7} />
                                {/if}
                                <span>{$t('menuBar.icon.choose')}</span>
                              </button>
                            </div>
                            {#if pickingIcon}
                              <IconPicker
                                on:select={(e) => onIconSelect(menu.id, item.id, child.id, e)}
                                on:clear={() => onIconClear(menu.id, item.id, child.id)}
                              />
                            {/if}
                          </div>
                        {/if}
                      </div>
                    {/each}
                  </div>
                  <div class="add-row-btns">
                    <button type="button" class="mini" on:click={() => addItem(menu.id, item.id)}>
                      <IconPlus size={14} stroke={1.7} /><span>{$t('menuBar.addEntry')}</span>
                    </button>
                    <button type="button" class="mini" on:click={() => addSeparator(menu.id, item.id)}>
                      <IconSeparatorHorizontal size={14} stroke={1.7} /><span>{$t('menuBar.addSeparator')}</span>
                    </button>
                  </div>
                </div>
              {/if}
            </div>
          {/each}
        </div>

        <div class="add-row-btns">
          <button type="button" class="mini" on:click={() => addItem(menu.id, null)}>
            <IconPlus size={14} stroke={1.7} /><span>{$t('menuBar.addEntry')}</span>
          </button>
          <button type="button" class="mini" on:click={() => addSub(menu.id)}>
            <IconFolderPlus size={14} stroke={1.7} /><span>{$t('menuBar.addSubmenu')}</span>
          </button>
          <button type="button" class="mini" on:click={() => addSeparator(menu.id, null)}>
            <IconSeparatorHorizontal size={14} stroke={1.7} /><span>{$t('menuBar.addSeparator')}</span>
          </button>
        </div>
      </div>
    {/if}
  {/each}
</div>

<style>
  .editor {
    position: relative;
    z-index: 230;
    background: var(--surface-sunken);
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .bar {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    min-height: 30px;
    /* starts past the macOS traffic lights like the bar it replaces; the inset
       is zero elsewhere. */
    padding: var(--space-1) var(--space-2) var(--space-1) max(var(--space-2), var(--titlebar-inset));
  }

  .chips {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    flex-wrap: wrap;
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    padding: 2px 2px 2px var(--space-1);
    border: var(--hairline) solid transparent;
    border-radius: var(--radius-control);
  }

  .chip.active {
    background: var(--surface-hover);
    border-color: var(--border-subtle);
  }

  .chip.dimmed .chip-title {
    opacity: 0.45;
  }

  .chip-title {
    padding: var(--space-1) var(--space-2);
    border: none;
    background: transparent;
    color: var(--text-primary);
    font-size: var(--fz-label);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .add-chip {
    display: inline-flex;
    padding: var(--space-1);
    border: var(--hairline) dashed var(--border-default);
    background: transparent;
    color: var(--text-secondary);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .add-chip:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .spacer {
    flex: 1;
  }

  .txt-btn,
  .done-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-1) var(--space-2);
    border: var(--hairline) solid var(--border-default);
    background: var(--surface-raised);
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .txt-btn:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .txt-btn.danger {
    border-color: var(--danger);
    color: var(--danger);
  }

  .done-btn {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--accent-fg);
  }

  .grip {
    display: inline-flex;
    color: var(--text-tertiary);
    cursor: grab;
  }

  .panel {
    padding: var(--space-2) var(--space-3) var(--space-3);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .panel-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding-bottom: var(--space-2);
  }

  .menu-name {
    font-size: var(--fz-label);
    color: var(--text-primary);
    font-weight: 500;
  }

  .name-input,
  .text-input,
  .rows :global(.action-select) {
    padding: var(--space-1) var(--space-2);
    font-size: var(--fz-label);
  }

  .rows {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
  }

  .row.dimmed .row-label {
    opacity: 0.45;
  }

  .glyph {
    display: inline-flex;
    color: var(--text-tertiary);
    flex: none;
  }

  .row-label {
    font-size: var(--fz-label);
    color: var(--text-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .row-label.muted {
    color: var(--text-tertiary);
    font-style: italic;
  }

  .badge {
    padding: 0 var(--space-1);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
  }

  .expander {
    display: inline-flex;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }

  .icon-btn {
    display: inline-flex;
    padding: var(--space-1);
    border: none;
    background: transparent;
    color: var(--text-secondary);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .icon-btn:hover,
  .icon-btn.on {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .icon-btn.danger:hover {
    color: var(--danger);
  }

  .sub {
    padding: var(--space-1) 0 var(--space-1) var(--space-5);
  }

  .edit-form {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-2) 0 var(--space-2) var(--space-5);
  }

  .edit-row {
    display: flex;
    gap: var(--space-2);
    align-items: center;
    flex-wrap: wrap;
  }

  .text-input {
    flex: 1;
    min-width: 140px;
  }

  .icon-choose,
  .mini {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-1) var(--space-2);
    border: var(--hairline) solid var(--border-default);
    background: var(--surface-raised);
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .icon-choose:hover,
  .mini:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .add-row-btns {
    display: flex;
    gap: var(--space-2);
    padding-top: var(--space-2);
  }
</style>
