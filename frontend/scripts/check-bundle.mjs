// Holds the main window's startup code in dist/ to a budget, and exits non-zero
// when it grows past it. Run it after `pnpm build`.
//
// The startup code is the entry chunk and App.svelte's chunk, which main.ts
// loads before it mounts the main window, with every chunk they import
// statically. Lazily loaded chunks are not counted. The budgets sit a little
// above the sizes they were set at, so a change that adds startup code has to
// move them on purpose.

import { existsSync, readFileSync, statSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const KiB = 1024
const entryBudget = 40 * KiB
const mainWindowBudget = 1760 * KiB

const dist = join(dirname(fileURLToPath(import.meta.url)), '..', 'dist')
const assets = join(dist, 'assets')

if (!existsSync(join(dist, 'index.html'))) {
  console.error('check-bundle: no dist/index.html, run pnpm build first')
  process.exit(1)
}

const html = readFileSync(join(dist, 'index.html'), 'utf8')
const entry = /<script[^>]+src="\/assets\/([^"]+\.js)"/.exec(html)?.[1]
if (!entry) {
  console.error('check-bundle: no entry script in dist/index.html')
  process.exit(1)
}

// static imports in rollup's output: `import"./a.js"`, `import{x}from"./b.js"`
// and the same with export. A dynamic import() has a parenthesis before its
// quote and is not matched.
const staticImport = /(?:^|[;\s}])(?:import|export)\s*(?:[\w$*{}\s,]*?from\s*)?["']\.\/([^"']+\.js)["']/g

const entryCode = readFileSync(join(assets, entry), 'utf8')
const app = /["'`]\.\/(App-[\w-]+\.js)["'`]/.exec(entryCode)?.[1]
if (!app) {
  console.error('check-bundle: the entry chunk does not load an App chunk')
  process.exit(1)
}

const seen = new Set()
const queue = [entry, app]
while (queue.length > 0) {
  const file = queue.pop()
  if (seen.has(file)) {
    continue
  }
  seen.add(file)
  for (const m of readFileSync(join(assets, file), 'utf8').matchAll(staticImport)) {
    queue.push(m[1])
  }
}

const entrySize = statSync(join(assets, entry)).size
const mainWindow = [...seen].reduce((sum, file) => sum + statSync(join(assets, file)).size, 0)
const kib = (n) => `${(n / KiB).toFixed(1)} KiB`

console.log(`entry chunk ${kib(entrySize)} of ${kib(entryBudget)}`)
console.log(`main window startup ${kib(mainWindow)} in ${seen.size} chunks, of ${kib(mainWindowBudget)}`)

let failed = false
if (entrySize > entryBudget) {
  console.error(`check-bundle: the entry chunk is ${kib(entrySize)}, over its ${kib(entryBudget)} budget`)
  failed = true
}
if (mainWindow > mainWindowBudget) {
  console.error(`check-bundle: the main window's startup code is ${kib(mainWindow)}, over its ${kib(mainWindowBudget)} budget`)
  failed = true
}
process.exit(failed ? 1 : 0)
