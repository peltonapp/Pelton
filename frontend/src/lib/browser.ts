// browser.ts opens links in the system browser. Components call openURL rather
// than the wails runtime directly, so tests can mock this one module.

import { Browser } from '@wailsio/runtime'

export function openURL(url: string): void {
  if (!url) {
    return
  }
  void Browser.OpenURL(url).catch(() => {})
}
