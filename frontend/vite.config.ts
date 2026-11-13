import {defineConfig} from 'vitest/config'
import {svelte} from '@sveltejs/vite-plugin-svelte'
import {fileURLToPath} from 'node:url'

// @tabler/icons ships its full icon geometry as tabler-nodes-outline.json, but
// the package's exports map only exposes ./icons/*, so the file is aliased here
// to a stable specifier. The icon picker lazy-imports it, keeping the ~2MB blob
// in its own chunk and out of the main bundle.
const tablerNodesOutline = fileURLToPath(
  new URL('./node_modules/@tabler/icons/tabler-nodes-outline.json', import.meta.url),
)

// @fontsource ships every face twice, as woff2 and as woff, and its css names
// both: `src: url(...woff2) format('woff2'), url(...woff) format('woff')`. A
// browser takes the first format it supports, so the woff copy is only ever
// read by one that cannot do woff2. The three webviews Pelton runs in
// (WKWebView, WebView2, WebKitGTK) have all supported woff2 for years, so every
// woff file here is weight nobody downloads.
//
// It is worth removing rather than ignoring: the Han face alone is 1.1 MB a
// weight, and shipping the dead copy of it costs more than the whole Arabic
// face does. Dropping them leaves the css pointing at a url that is not there,
// which is harmless because nothing ever requests it.
const dropLegacyWoff = {
  name: 'drop-legacy-woff',
  generateBundle(_options: unknown, bundle: Record<string, unknown>) {
    for (const file of Object.keys(bundle)) {
      if (file.endsWith('.woff')) {
        delete bundle[file]
      }
    }
  },
}

// on the dev server a flag is a plain path rather than the build's url
// template; flagurl.dev.ts says why.
const flagUrlDev = {
  name: 'flag-url-dev',
  apply: 'serve' as const,
  enforce: 'pre' as const,
  async resolveId(this: { resolve: (s: string, i?: string, o?: object) => Promise<{ id: string } | null> }, source: string, importer?: string) {
    if (!source.endsWith('/flagurl')) {
      return null
    }
    const resolved = await this.resolve(source, importer, { skipSelf: true })
    return resolved ? resolved.id.replace(/flagurl\.ts$/, 'flagurl.dev.ts') : null
  },
}

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [svelte(), dropLegacyWoff, flagUrlDev],
  resolve: {
    alias: {
      'tabler-nodes-outline': tablerNodesOutline,
    },
    // under vitest, svelte otherwise resolves to its server build, where mount()
    // does not exist and every component test fails before it renders.
    ...(process.env.VITEST ? { conditions: ['browser'] } : {}),
  },
  build: {
    // the flag set is globbed whole so a new locale needs no asset work, and
    // most of its svgs are small enough that vite would inline every one of
    // them into the picker's chunk. They stay files, so only the handful
    // actually rendered is ever read.
    assetsInlineLimit: (file) => (file.includes('flag-icons/flags/') ? false : undefined),
  },
  test: {
    // jsdom throughout rather than per-file: the pure modules do not care, and
    // one environment means a test can reach for the DOM without moving file.
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.ts'],
    alias: {
      '@wailsio/runtime': fileURLToPath(new URL('./src/test/wailsruntime.ts', import.meta.url)),
    },
  },
})
