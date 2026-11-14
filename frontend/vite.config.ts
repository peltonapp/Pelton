import {defineConfig} from 'vitest/config'
import {svelte} from '@sveltejs/vite-plugin-svelte'
import {fileURLToPath} from 'node:url'
import fs from 'node:fs'
import path from 'node:path'

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

// the pdf viewer's support files (cmaps, standard fonts, colour profiles and
// image decoders) are fetched by pdf.js at runtime, so they ship as files under
// /pdfjs/ rather than going through the module graph. quickjs is left out: it
// only runs scripts embedded in a pdf, which the viewer never does.
const pdfjsRoot = fileURLToPath(new URL('./node_modules/pdfjs-dist/', import.meta.url))
const pdfjsDirs = ['cmaps', 'standard_fonts', 'iccs', 'wasm']
const pdfjsSkip = (name: string) => name.startsWith('quickjs') || name.startsWith('LICENSE')
const pdfjsAssets = {
  name: 'pdfjs-assets',
  configureServer(server: { middlewares: { use: (fn: (req: { url?: string }, res: { setHeader: (k: string, v: string) => void, end: (b: Buffer) => void }, next: () => void) => void) => void } }) {
    server.middlewares.use((req, res, next) => {
      const m = /^\/pdfjs\/([a-z_]+)\/([A-Za-z0-9_.-]+)$/.exec((req.url ?? '').split('?')[0])
      if (!m || !pdfjsDirs.includes(m[1]) || pdfjsSkip(m[2])) {
        next()
        return
      }
      const file = path.join(pdfjsRoot, m[1], m[2])
      if (!fs.existsSync(file)) {
        next()
        return
      }
      if (file.endsWith('.wasm')) {
        res.setHeader('Content-Type', 'application/wasm')
      }
      res.end(fs.readFileSync(file))
    })
  },
  generateBundle(this: { emitFile: (f: { type: 'asset', fileName: string, source: Uint8Array }) => string }) {
    for (const dir of pdfjsDirs) {
      for (const name of fs.readdirSync(path.join(pdfjsRoot, dir))) {
        if (pdfjsSkip(name)) {
          continue
        }
        this.emitFile({ type: 'asset', fileName: `pdfjs/${dir}/${name}`, source: fs.readFileSync(path.join(pdfjsRoot, dir, name)) })
      }
    }
  },
}

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [svelte(), dropLegacyWoff, flagUrlDev, pdfjsAssets],
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
  },
})
