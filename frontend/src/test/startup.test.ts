// The main window's startup code is what main.ts and App.svelte reach through
// static imports. The windows, spaces, editors and dialogs below load on first
// use, and a static import of any of them would put it back on the startup
// path without failing anything else.
import { describe, expect, it } from 'vitest'

const sources = import.meta.glob('/src/**/*.{ts,svelte}', { query: '?raw', import: 'default', eager: true }) as Record<string, string>

const importPattern = /(?:^|[\s;])(?:import|export)\s+(?!type\s)(?:[^'"`;]*?\sfrom\s+)?['"]([^'"]+)['"]/g

function scriptOf(file: string): string {
  const text = sources[file]
  if (!file.endsWith('.svelte')) {
    return text
  }
  return [...text.matchAll(/<script\b[^>]*>([\s\S]*?)<\/script\b[^>]*>/gi)].map((m) => m[1]).join('\n')
}

function join(dir: string, rel: string): string {
  const parts = dir.split('/')
  for (const part of rel.split('/')) {
    if (part === '..') {
      parts.pop()
    } else if (part !== '.') {
      parts.push(part)
    }
  }
  return parts.join('/')
}

function resolve(from: string, spec: string): string | null {
  const bare = spec.split('?')[0]
  if (!bare.startsWith('.')) {
    return null
  }
  const base = join(from.slice(0, from.lastIndexOf('/')), bare)
  for (const candidate of [base, `${base}.ts`, `${base}/index.ts`]) {
    if (candidate in sources) {
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
  const out = [file]
  for (let at = graph.via.get(file); at; at = graph.via.get(at)) {
    out.unshift(at)
  }
  return out.join(' -> ')
}

const windows = ['App.svelte', 'MessageWindow.svelte', 'ComposeWindow.svelte', 'OutboxWindow.svelte', 'QuickComposeWindow.svelte']

const entry = staticGraph('/src/main.ts')
// main.ts loads App.svelte before it mounts the main window.
const main = staticGraph('/src/App.svelte')

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
  'components/detail/PromiseStrip.svelte',
  'components/detail/EncryptionIndicator.svelte',
  'components/detail/InviteCard.svelte',
  'components/detail/DatePeek.svelte',
  'components/vault/VaultDialogs.svelte',
  'components/vault/VaultUnlockDialog.svelte',
  'components/vault/VaultCreateDialog.svelte',
  'components/vault/VaultConsentDialog.svelte',
  'components/settings2/VaultCard.svelte',
  'components/settings2/CliCard.svelte',
  'lib/cliapi.ts',
  'stores/mailvault.ts',
  'lib/mailvaultapi.ts',
]

const lazyPackages = ['@codemirror/', 'codemirror', '@replit/codemirror-vim', '@tiptap/', 'pdfjs-dist', 'tabler-nodes-outline', '@tabler/icons/']

describe('startup imports', () => {
  it('follows the imports it is meant to', () => {
    expect(entry.files.has('/src/Root.svelte')).toBe(true)
    expect(main.files.has('/src/components/list/MessageList.svelte')).toBe(true)
    expect(main.files.has('/src/lib/i18n.ts')).toBe(true)
    expect(main.packages.has('svelte')).toBe(true)
  })

  it.each(windows)('main.ts does not statically import %s', (rel) => {
    const file = `/src/${rel}`
    expect(file in sources).toBe(true)
    expect(entry.files.has(file) ? chain(entry, file) : '').toBe('')
  })

  it.each(lazyModules)('the main window does not statically import %s', (rel) => {
    const file = `/src/${rel}`
    expect(file in sources).toBe(true)
    for (const graph of [entry, main]) {
      expect(graph.files.has(file) ? chain(graph, file) : '').toBe('')
    }
  })

  it('reaches no heavy package statically', () => {
    for (const graph of [entry, main]) {
      const heavy = [...graph.packages].filter((p) => lazyPackages.some((h) => p === h || p.startsWith(h)))
      expect(heavy.map((p) => `${p} via ${graph.via.get(p) ?? ''}`)).toEqual([])
    }
  })
})
