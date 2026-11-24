import type { SettingsIndexEntry } from '../../../lib/settings2'

const entries: SettingsIndexEntry[] = [
  { page: 'feeds', id: 'feeds-interval', labelKey: 'feedSettings.interval', keywords: 'feeds rss atom refresh interval fetch how often' },
  { page: 'feeds', id: 'feeds-refresh-now', labelKey: 'feeds.refreshAll', keywords: 'feeds rss refresh now fetch update' },
  { page: 'feeds', id: 'feeds-keep-days', labelKey: 'feedSettings.keepDays', keywords: 'feeds rss retention keep days old delete prune' },
  { page: 'feeds', id: 'feeds-keep-count', labelKey: 'feedSettings.keepCount', keywords: 'feeds rss retention keep entries per feed limit' },
  { page: 'feeds', id: 'feeds-images', labelKey: 'feedSettings.images', keywords: 'feeds rss images remote content load pictures privacy' },
  { page: 'feeds', id: 'feeds-mark-scroll', labelKey: 'feedSettings.markOnScroll', keywords: 'feeds rss mark read scroll' },
  { page: 'feeds', id: 'feeds-notify', labelKey: 'feedSettings.notify', keywords: 'feeds rss notifications alerts new entries' },
  { page: 'feeds', id: 'feeds-opml', labelKey: 'feedSettings.opml', keywords: 'feeds rss opml import export subscriptions backup' },
]

export default entries
