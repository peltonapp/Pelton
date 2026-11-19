// palette-feeds.ts puts the Feeds space into the command palette: going to
// Feeds and to its smart views, adding a feed, refreshing, opening a feed by
// name, OPML import and export, and the Feeds settings. the entries work from
// any space; one that needs the Feeds screen opens it first.

import { get } from 'svelte/store'
import { IconDownload, IconPlus, IconRefresh, IconRss, IconSettings, IconUpload } from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import type { CommandStep, PaletteCommand } from './commands'
import { exportOpml, importOpml } from './api'
import { smartViews } from './feedlogic'
import { openSpace, runInSpace } from '../stores/space'
import { openSettingsAt } from '../stores/settingsnav'
import { menuHint } from '../stores/shortcuts'
import { feedSidebar, loadFeedSidebar, refreshAllFeeds, showScope } from '../stores/feeds'
import { errorMessage, toastError, toastSuccess } from '../stores/toast'
import type { FeedScope } from './types'

function show(scope: FeedScope): void {
  openSpace('feeds')
  void showScope(scope, false)
}

// a step listing the feeds; picking one shows its entries.
function feedsStep(t: (key: string) => string): CommandStep {
  return {
    label: t('palette.feeds.find'),
    placeholder: t('palette.feeds.findPlaceholder'),
    items: get(feedSidebar).feeds.map((f) => ({
      id: `feeds:show:${f.id}`,
      group: 'feeds' as const,
      label: f.title,
      keywords: f.url,
      icon: IconRss,
      run: () => show({ kind: 'feed', id: f.id }),
    })),
  }
}

async function runImport(t: (key: string) => string): Promise<void> {
  try {
    const res = await importOpml()
    if (res.added > 0 || res.skipped > 0) {
      toastSuccess(t('feeds.imported').replace('{added}', String(res.added)).replace('{skipped}', String(res.skipped)))
    }
    await loadFeedSidebar()
  } catch (err) {
    toastError(errorMessage(err))
  }
}

async function runExport(t: (key: string) => string): Promise<void> {
  try {
    if (await exportOpml()) {
      toastSuccess(t('feeds.exported'))
    }
  } catch (err) {
    toastError(errorMessage(err))
  }
}

registerPaletteSource('feeds', (t) => {
  const out: PaletteCommand[] = [
    {
      id: 'feeds:open',
      group: 'feeds',
      label: t('palette.feeds.open'),
      hint: menuHint('go-feeds'),
      icon: IconRss,
      run: () => openSpace('feeds'),
    },
    {
      id: 'feeds:add',
      group: 'feeds',
      label: t('feeds.addFeed'),
      keywords: t('palette.feeds.addKeywords'),
      icon: IconPlus,
      run: () => runInSpace('feeds', 'compose'),
    },
    {
      id: 'feeds:refresh',
      group: 'feeds',
      label: t('feeds.refreshAll'),
      icon: IconRefresh,
      run: () => void refreshAllFeeds(),
    },
    ...smartViews.map((kind) => ({
      id: `feeds:view:${kind}`,
      group: 'feeds' as const,
      label: `${t('space.feeds')}: ${t(`feeds.smart.${kind}`)}`,
      icon: IconRss,
      run: () => show({ kind }),
    })),
    {
      id: 'feeds:import',
      group: 'feeds',
      label: t('feeds.importOpml'),
      keywords: 'opml',
      icon: IconUpload,
      run: () => void runImport(t),
    },
    {
      id: 'feeds:export',
      group: 'feeds',
      label: t('feeds.exportOpml'),
      keywords: 'opml',
      icon: IconDownload,
      run: () => void runExport(t),
    },
    {
      id: 'feeds:settings',
      group: 'feeds',
      label: t('palette.feeds.settings'),
      icon: IconSettings,
      run: () => openSettingsAt('feeds'),
    },
  ]
  if (get(feedSidebar).feeds.length > 0) {
    out.push({
      id: 'feeds:find',
      group: 'feeds',
      label: t('palette.feeds.find'),
      icon: IconRss,
      run: () => feedsStep(t),
    })
  }
  return out
})
