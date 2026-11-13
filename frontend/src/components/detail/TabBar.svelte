<script lang="ts">
  // the reading-pane tab strip (#197). It only renders when a tab exists, so
  // anyone who never opens one never sees it and loses no vertical space.
  //
  // The first chip is the untabbed pane, which is where clicking a message in
  // the list always lands. After it come the parked messages, oldest first.
  import { fly } from 'svelte/transition'
  import { flip } from 'svelte/animate'
  import { IconX, IconMail, IconAlertTriangle } from '@tabler/icons-svelte'
  import { tabs, activeTabId, closeTab, focusTab, focusPane, reorderTabs } from '../../stores/tabs'
  import { prefs } from '../../stores/prefs'
  import { shortcutTitle } from '../../stores/shortcuts'
  import { reorder, type ReorderDetail, type TearOffDetail } from '../../lib/reorder'
  import { openMessageWindow } from '../../lib/api'
  import { errorMessage, toastError } from '../../stores/toast'
  import { t } from '../../lib/i18n'

  // a tab sliding in and out reads as one thing moving rather than the bar
  // jumping. Off entirely when the user asked for less motion.
  $: motion = $prefs.reduceMotion ? 0 : 140

  // middle-click closes a tab, the way it does everywhere else. The listener is
  // on auxclick rather than mousedown so the browser's autoscroll never starts.
  function onAux(event: MouseEvent, id: number): void {
    if (event.button === 1) {
      event.preventDefault()
      closeTab(id)
    }
  }

  // tabs drag into a new order, the way browser tabs do. The pane chip sits
  // outside the reorder container, so it can never be dragged out of first
  // place: it is where the list puts a message, not one of the parked ones.
  function onReorder(event: CustomEvent<ReorderDetail>): void {
    reorderTabs(event.detail.ids.map(Number))
  }

  // a tab dragged off the bar leaves the pane for a window of its own (#298),
  // the way a browser tab does. The tab closes only once the window opened.
  async function onTearOff(event: CustomEvent<TearOffDetail>): Promise<void> {
    const id = Number(event.detail.id)
    const tab = $tabs.find((candidate) => candidate.id === id)
    if (!tab || tab.stale) {
      return
    }
    try {
      await openMessageWindow(id, tab.label)
      closeTab(id)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // a drag is only a drag past a few pixels, so a plain press still selects.
  let dragging = false
</script>

<div class="bar" role="tablist" aria-label={$t('tabs.bar')}>
  <button
    type="button"
    role="tab"
    class="chip pane"
    class:on={$activeTabId === null}
    aria-selected={$activeTabId === null}
    title={$t('tabs.pane')}
    aria-label={$t('tabs.pane')}
    on:click={focusPane}
  >
    <IconMail size={14} stroke={1.7} />
  </button>

  <div
    class="strip"
    class:dragging
    use:reorder={{ axis: 'x', tearOff: true }}
    on:reorderstart={() => (dragging = true)}
    on:reorderend={() => (dragging = false)}
    on:reorder={onReorder}
    on:reordertearoff={onTearOff}
  >
    {#each $tabs as tab (tab.id)}
      <div
        class="chip"
        class:on={$activeTabId === tab.id}
        class:stale={tab.stale}
        data-reorder-id={tab.id}
        in:fly={{ y: -6, duration: motion }}
        out:fly={{ y: -6, duration: motion }}
        animate:flip={{ duration: motion }}
      >
        <button
          type="button"
          role="tab"
          class="label"
          data-reorder-handle
          aria-selected={$activeTabId === tab.id}
          title={tab.stale ? $t('tabs.stale') : tab.label}
          on:click={() => focusTab(tab.id)}
          on:auxclick={(e) => onAux(e, tab.id)}
        >
          {#if tab.stale}
            <span class="warn"><IconAlertTriangle size={12} stroke={1.9} /></span>
          {/if}
          <span class="text">{tab.label || $t('tabs.untitled')}</span>
        </button>
        <button
          type="button"
          class="close"
          aria-label={$t('tabs.close')}
          title={$shortcutTitle($t('tabs.close'), 'close-tab', 'close-window')}
          on:click={() => closeTab(tab.id)}
        >
          <IconX size={12} stroke={2} />
        </button>
      </div>
    {/each}
  </div>
</div>

<style>
  /* tabs shrink to a readable minimum and the bar scrolls after that, so there
     is no cap on how many can be open. */
  .bar {
    display: flex;
    align-items: stretch;
    gap: var(--space-1);
    padding: var(--space-1) var(--space-2);
    border-bottom: var(--hairline) solid var(--border-subtle);
    background: var(--surface-sunken);
    overflow-x: auto;
    scrollbar-width: thin;
  }

  /* the tabs live in their own row inside the bar: the reorder action moves
     the children of the element it is on, and the pane chip is not one of
     them. */
  .strip {
    display: flex;
    align-items: stretch;
    gap: var(--space-1);
    min-width: 0;
  }

  /* while a tab is being dragged the labels must not select under the cursor. */
  .strip.dragging {
    user-select: none;
  }

  .chip {
    display: flex;
    align-items: center;
    flex: 0 1 auto;
    /* a tab shrinks to this before the bar starts scrolling, which is about
       where a subject stops being worth reading. */
    min-width: 96px;
    max-width: 180px;
    border: var(--hairline) solid transparent;
    border-radius: var(--radius-control);
    color: var(--text-tertiary);
  }

  .chip:hover {
    background: var(--surface-hover);
  }

  /* the app-wide reduce-motion switch strips these in style.css. */
  .chip,
  .pane,
  .close {
    transition:
      background 0.12s ease,
      color 0.12s ease;
  }

  .chip.on {
    background: var(--surface-raised);
    border-color: var(--border-default);
    color: var(--text-primary);
  }

  .chip.stale .text {
    text-decoration: line-through;
  }

  /* the pane chip holds one small icon, so it is sized to it. Without this it
     would inherit the tab width rules and sit in a wide empty box. */
  .pane {
    flex: 0 0 auto;
    min-width: 0;
    max-width: none;
    padding: var(--space-1) var(--space-2);
    background: transparent;
    cursor: var(--cursor-action);
  }
  .pane.on {
    background: var(--surface-raised);
  }

  /* the label is the only part that gives: the warning icon and the close
     button keep their size, so the subject clips instead of running under
     them. */
  .label {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    padding: var(--space-1) 0 var(--space-1) var(--space-2);
    border: none;
    background: transparent;
    color: inherit;
    font-family: var(--font-ui);
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
  }

  .text {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-align: start;
  }

  .warn {
    display: inline-flex;
    flex-shrink: 0;
    color: var(--warning);
  }

  .close {
    display: inline-flex;
    align-items: center;
    flex: 0 0 auto;
    padding: var(--space-1);
    margin: 0 var(--space-1);
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }
  .close:hover {
    color: var(--text-primary);
    background: var(--surface-sunken);
  }
</style>
