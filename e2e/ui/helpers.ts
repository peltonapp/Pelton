import type { Browser, Locator, Page } from '@playwright/test'
import { basename, dirname, resolve } from 'node:path'

export function messageList(page: Page): Locator {
  return page.getByRole('listbox', { name: 'Messages' })
}

// countMessages reads "N messages" under the list: the messages stored
// locally for the folder, which grows when a backfill fetches older mail.
export async function countMessages(page: Page): Promise<number> {
  const text = await page.getByText(/^\d+ messages?$/).first().innerText()
  return Number(text.replace(/\D/g, ''))
}

// SYNC_WINDOW is the default Messages to sync per folder: a first sync stores
// this many of a folder's newest messages, filling it in pages (50, then the
// rest), so a count up to it says nothing about backfill.
export const SYNC_WINDOW = 100

// waitStable polls read until it returns the same value for quietMs and
// returns it, so a test starts from a settled count instead of one that a
// first-sync page or an earlier backfill is still raising. A string read can
// join several counts so that all of them must hold still together.
export async function waitStable<T extends number | string>(
  read: () => T | Promise<T>,
  { quietMs = 5_000, timeoutMs = 90_000, intervalMs = 500 } = {},
): Promise<T> {
  const deadline = Date.now() + timeoutMs
  let value = await read()
  let since = Date.now()
  while (Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, intervalMs))
    const next = await read()
    if (next !== value) {
      value = next
      since = Date.now()
    } else if (Date.now() - since >= quietMs) {
      return value
    }
  }
  throw new Error(`still changing after ${timeoutMs} ms (last ${value})`)
}

// readerText is the body text of the open message in the reading pane, not
// the list snippet: a plain-text body is a <pre>, an HTML body an iframe.
export async function readerText(page: Page): Promise<string> {
  const detail = page.locator('section.detail')
  const plain = detail.locator('pre.body-plain')
  if (await plain.count()) return plain.first().innerText()
  if (await detail.locator('iframe.body-frame').count()) {
    return page.frameLocator('section.detail iframe.body-frame').locator('body').innerText()
  }
  return ''
}

export function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// assertE2EApp stops the suite before any test acts unless PELTON_URL serves
// an app built as an e2e channel (make e2e-app) that keeps its data in a
// Pelton-e2e* directory. Without it a wrong URL, e.g. the 34115 dev server of
// `make run`, would onboard, archive and change settings in Pelton-dev.
//
// The check must not run any of the target app's own code: the Svelte bundle
// loads preferences, restores tabs and the sidebar and may write settings as it
// starts. So the page loads only the index HTML and the two scripts the Wails
// dev server injects into it (runtime.js builds window.go from the bindings,
// ipc.js connects the /wails/ipc WebSocket, which page.route does not see).
// Every other request, the app bundle and Vite's client included, is aborted.
//
// Both calls are read-only bound methods. GetDiagnostics starts with
// "Pelton <version> (<channel>)". GetLogStatus().dir is <data dir>/logs, where
// <data dir> is the directory the store opened; if the store did not open it
// falls back to the channel's default directory, so a diagnostics report that
// says the database did not open is refused as well.
const IDENTITY_PATHS = new Set(['/', '/index.html', '/wails/runtime.js', '/wails/ipc.js'])

export async function assertE2EApp(browser: Browser): Promise<void> {
  const baseURL = process.env.PELTON_URL
  if (!baseURL) refuse(baseURL, 'PELTON_URL is not set')
  const origin = new URL(baseURL).origin
  const context = await browser.newContext({ baseURL, serviceWorkers: 'block' })
  const blocked: string[] = []
  await context.route('**/*', (route) => {
    const url = new URL(route.request().url())
    if (url.origin === origin && IDENTITY_PATHS.has(url.pathname)) return route.continue()
    blocked.push(url.pathname)
    return route.abort('blockedbyclient')
  })
  try {
    const page = await context.newPage()
    await page.goto('/')
    await page
      .waitForFunction(
        () => {
          const app = (window as unknown as { go?: { desktop?: { App?: Record<string, unknown> } } }).go?.desktop?.App
          return typeof app?.GetDiagnostics === 'function' && typeof app?.GetLogStatus === 'function'
        },
        null,
        { timeout: 60_000 },
      )
      .catch(() => refuse(baseURL, `it exposes no Wails bindings for desktop.App (blocked: ${blocked.join(', ') || 'none'})`))
    const { diagnostics, logDir } = await page.evaluate(async () => {
      type Bound = { GetDiagnostics(): Promise<string>; GetLogStatus(): Promise<{ dir: string }> }
      const app = (window as unknown as { go: { desktop: { App: Bound } } }).go.desktop.App
      return { diagnostics: await app.GetDiagnostics(), logDir: (await app.GetLogStatus()).dir }
    })
    const firstLine = diagnostics.split('\n')[0]
    const channel = /^Pelton \S+ \((e2e(?:-[^)\s]+)?)\)$/.exec(firstLine)?.[1]
    if (!channel) refuse(baseURL, `its build channel is not e2e (${firstLine})`)
    if (/the database did not open/.test(diagnostics)) refuse(baseURL, 'its database did not open')
    if (!logDir) refuse(baseURL, 'it reports no data directory')
    const dataDir = dirname(logDir)
    if (basename(dataDir) !== `Pelton-${channel}`) refuse(baseURL, `its data directory is ${dataDir}, not Pelton-${channel}`)
    const db = process.env.E2E_DB
    if (db && resolve(dirname(db)) !== resolve(dataDir)) {
      refuse(baseURL, `its data directory is ${dataDir}, but E2E_DB is ${db}`)
    }
  } finally {
    await context.close()
  }
}

function refuse(baseURL: string | undefined, why: string): never {
  throw new Error(`refusing to run the e2e suite against ${baseURL}: ${why}. Start the app with make e2e-app (e2e/run.sh does).`)
}
