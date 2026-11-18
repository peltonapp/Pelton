// The main window's startup code is what main.ts and App.svelte reach through
// static imports. The windows, spaces, editors and dialogs below load on first
// use, and a static import of any of them would put it back on the startup
// path without failing anything else.
import { describe, expect, it } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const src = path.dirname(path.dirname(fileURLToPath(import.meta.url)))

const importPattern = /(?:^|[\s;])(?:import|export)\s+(?!type\s)(?:[^'"`;]*?\sfrom\s+)?['"]([^'"]+)['"]/g

function scriptOf(file: string): string {
  const text = fs.readFileSync(file, 'utf8')
  if (!file.endsWith('.svelte')) {
    return text
  }
  return [...text.matchAll(/<script[^>]*>([\s\S]*?)<\/script>/g)].map((m) => m[1]).join('\n')
}

function resolve(from: string, spec: string): string | null {
  const bare = spec.split('?')[0]
  if (!bare.startsWith('.')) {
    return null
  }
  const base = path.resolve(path.dirname(from), bare)
  for (const candidate of [base, `${base}.ts`, `${base}/index.ts`]) {
    if (fs.existsSync(candidate) && fs.statSync(candidate).isFile()) {
      return candidate
    }
  }
  return null
}

interface Graph {
  files: Set<string>
  packages: Set<string>
  via: Map<string, string>
}

/** staticGraph walks static imports from entry: the files it reaches and the packages they name. */
function staticGraph(entry: string): Graph {
  const files = new Set([entry])
  const packages = new Set<string>()
  const via = new Map<string, string>()
  const queue = [entry]
  while (queue.length > 0) {
    const file = queue.shift()!
    if (!/\.(ts|svelte|js)$/.test(file)) {
      continue
    }
    const code = scriptOf(file).replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '')
    for (const m of code.matchAll(importPattern)) {
      const spec = m[1]
      const target = resolve(file, spec)
      if (target === null) {
        if (!spec.startsWith('.')) {
          packages.add(spec.split('?')[0])
          via.set(spec.split('?')[0], file)
        }
        continue
      }
      if (!files.has(target)) {
        files.add(target)
        via.set(target, file)
        queue.push(target)
      }
    }
  }
  return { files, packages, via }
}

function chain(graph: Graph, file: string): string {
  const out = [path.relative(src, file)]
  for (let at = graph.via.get(file); at; at = graph.via.get(at)) {
    out.unshift(path.relative(src, at))
  }
  return out.join(' -> ')
}

const windows = ['App.svelte', 'MessageWindow.svelte', 'ComposeWindow.svelte', 'OutboxWindow.svelte', 'QuickComposeWindow.svelte']

const entry = staticGraph(path.join(src, 'main.ts'))
// main.ts loads App.svelte before it mounts the main window.
const main = staticGraph(path.join(src, 'App.svelte'))

const lazyModules = [
  'components/compose/Compose.svelte',
  'components/compose/Composer2.svelte',
  'components/calendar/CalendarScreen.svelte',
  'components/notes/NotesScreen.svelte',
  'components/notes/NoteGraph.svelte',
  'components/chat/ChatSpace.svelte',
  'components/tasks/TasksScreen.svelte',
  'components/projects/ProjectsScreen.svelte',
  'components/feeds/FeedsScreen.svelte',
  'components/contacts/ContactsSpace.svelte',
  'components/automations/AutomationsSpace.svelte',
  'components/settings/SettingsPanel.svelte',
  'components/settings2/SettingsScreen.svelte',
  'components/onboarding/Onboarding.svelte',
  'components/onboarding/Setup.svelte',
  'components/wizard/AddMailboxWizard.svelte',
  'components/attachments/AttachmentsBrowser.svelte',
  'components/common/MenuBarEditor.svelte',
  'components/common/CommandPalette.svelte',
]

const lazyPackages = ['@codemirror/', 'codemirror', '@replit/codemirror-vim', '@tiptap/', 'pdfjs-dist', 'tabler-nodes-outline', '@tabler/icons/']

describe('startup imports', () => {
  it('follows the imports it is meant to', () => {
    expect(entry.files.has(path.join(src, 'Root.svelte'))).toBe(true)
    expect(main.files.has(path.join(src, 'components/list/MessageList.svelte'))).toBe(true)
    expect(main.files.has(path.join(src, 'lib/i18n.ts'))).toBe(true)
    expect(main.packages.has('svelte')).toBe(true)
  })

  it.each(windows)('main.ts does not statically import %s', (rel) => {
    const file = path.join(src, rel)
    expect(fs.existsSync(file)).toBe(true)
    expect(entry.files.has(file) ? chain(entry, file) : '').toBe('')
  })

  it.each(lazyModules)('the main window does not statically import %s', (rel) => {
    const file = path.join(src, rel)
    expect(fs.existsSync(file)).toBe(true)
    for (const graph of [entry, main]) {
      expect(graph.files.has(file) ? chain(graph, file) : '').toBe('')
    }
  })

  it('reaches no heavy package statically', () => {
    for (const graph of [entry, main]) {
      const heavy = [...graph.packages].filter((p) => lazyPackages.some((h) => p === h || p.startsWith(h)))
      expect(heavy.map((p) => `${p} via ${path.relative(src, graph.via.get(p) ?? '')}`)).toEqual([])
    }
  })
})
