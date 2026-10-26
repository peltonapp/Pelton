import { expect, test, type Locator, type Page } from '@playwright/test'
import { readFileSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { SYNC_WINDOW, assertE2EApp, countMessages, escapeRegExp, messageList, readerText, waitStable } from './helpers'

const here = dirname(fileURLToPath(import.meta.url))
const manifest = JSON.parse(readFileSync(join(here, '../manifest.json'), 'utf8')) as {
  aliceCount: number
  bobCount: number
  aliceShortSubject: string
  aliceLargeSubject: string
  shortBody: string
  largePrefix: string
}

const timings: Record<string, number | string> = {}

test.describe.configure({ mode: 'serial' })

// Before anything onboards or adds a mailbox: PELTON_URL must be the e2e app.
test.beforeAll(async ({ browser }) => {
  await assertE2EApp(browser)
})

test.beforeEach(async ({ page }, info) => {
  if (info.title.startsWith('onboarding')) return
  await page.goto('/')
  await messageList(page).waitFor()
})

test.afterAll(() => {
  const lines = [
    '# Pelton e2e timings',
    '',
    'Thresholds are not assertions. The numbers are from this run.',
    '',
    ...Object.entries(timings).map(([k, v]) => `- ${k}: ${v}`),
    '',
  ]
  writeFileSync(join(here, '../report.md'), lines.join('\n'))
})

async function readyApp(page: Page) {
  await page.goto('/')
  const start = page.getByRole('button', { name: 'Get started' })
  try {
    await start.waitFor({ timeout: 15_000 })
  } catch {
    await page.reload()
    await start.waitFor()
  }
}

async function throughOnboarding(page: Page) {
  await page.getByRole('button', { name: 'Get started' }).click()
  await page.getByRole('checkbox', { name: 'I have read this and understand it.' }).click()
  await page.getByRole('button', { name: 'Continue' }).click()
  for (let i = 0; i < 15; i++) {
    const add = page.getByRole('button', { name: 'Add a mailbox Sign in to an account by hand' })
    if (await add.isVisible().catch(() => false)) return
    const skipDefault = page.getByRole('heading', { name: 'Make Pelton your default mail app' })
    if (await skipDefault.isVisible().catch(() => false)) {
      await page.getByRole('button', { name: 'Skip for now' }).click()
      continue
    }
    await page.getByRole('button', { name: /^Continue/ }).click()
  }
  throw new Error('onboarding did not reach the mailbox step')
}

async function fillOtherAccount(page: Page, email: string, password: string) {
  await page.getByRole('button', { name: 'Other (IMAP / SMTP)' }).click()
  await page.getByRole('textbox', { name: 'Email', exact: true }).fill(email)
  await page.getByRole('textbox', { name: 'Password', exact: true }).fill(password)
  await page.getByRole('textbox', { name: 'IMAP host' }).fill('127.0.0.1')
  await page.getByRole('textbox', { name: 'SMTP host' }).fill('127.0.0.1')
  await expect(page.getByRole('spinbutton', { name: 'Port' }).first()).toHaveValue('993')
  await expect(page.getByRole('spinbutton', { name: 'Port' }).nth(1)).toHaveValue('465')
  await page.getByRole('button', { name: 'Test connection' }).click()
  await expect(page.getByText('Connection works.')).toBeVisible({ timeout: 30_000 })
  // The sidebar has its own "Add mailbox" button; take the dialog's.
  await page
    .getByRole('dialog', { name: 'Add mailbox' })
    .getByRole('button', { name: 'Add mailbox', exact: true })
    .click()
}

// accountInbox is the INBOX under one account in the sidebar: with Alice and
// Bob both on IMAP, a bare INBOX name matches both.
function accountInbox(page: Page, email: string): Locator {
  return page
    .locator('section.account')
    .filter({ has: page.getByRole('button', { name: email, exact: true }) })
    .getByRole('button', { name: /^INBOX/ })
}

test('onboarding, liability, and Alice as IMAP', async ({ page }) => {
  await readyApp(page)
  await throughOnboarding(page)
  await page.getByRole('button', { name: 'Add a mailbox Sign in to an account by hand' }).click()
  await fillOtherAccount(page, 'alice@example.org', 'alice-e2e')
  const started = Date.now()
  await page.getByRole('button', { name: 'Start using Pelton' }).click()
  const list = messageList(page)
  await expect(list.getByRole('option').first()).toBeVisible({ timeout: 60_000 })
  timings['alice first inbox rows ms'] = Date.now() - started
  await expect(page.getByRole('button', { name: 'alice@example.org' })).toBeVisible()
  const rows = await list.getByRole('option').count()
  expect(rows).toBeGreaterThan(0)
  expect(rows).toBeLessThan(manifest.aliceCount / 10)
  timings['alice dom rows after first paint'] = rows
  timings['alice server inbox count'] = manifest.aliceCount
})

test('scroll backfills older rows and a synced body opens', async ({ page }) => {
  const list = messageList(page)
  // The first sync stores only the newest window, so the local count starts
  // well below the server's. It fills that window in pages, so wait for it to
  // settle: growth up to the window is not backfill. Scrolling to the end must
  // then fetch older mail, past the window.
  const before = await waitStable(() => countMessages(page))
  expect(before).toBeLessThan(manifest.aliceCount)
  const started = Date.now()
  await expect
    .poll(
      async () => {
        await list.evaluate((el) => {
          el.scrollTop = el.scrollHeight
          el.dispatchEvent(new Event('scroll', { bubbles: true }))
        })
        return countMessages(page)
      },
      { timeout: 60_000, intervals: [800] },
    )
    .toBeGreaterThan(Math.max(before, SYNC_WINDOW))
  timings['scroll until older rows backfilled ms'] = Date.now() - started
  timings['alice local count before and after backfill'] = `${before} -> ${await countMessages(page)}`
  // Virtualized: the DOM holds a slice, not every loaded row.
  expect(await list.getByRole('option').count()).toBeLessThan(manifest.aliceCount / 10)

  // Deep in the list, away from the seeded extremes at the top.
  const openStarted = Date.now()
  await list.getByRole('option').first().click()
  await expect.poll(() => readerText(page)).toMatch(/abcdefghijklmnopqrstuvwxyz/)
  timings['open synced message body ms'] = Date.now() - openStarted
  await expect(list.getByRole('option', { selected: true })).toBeVisible()
})

test('search, folders, and the seeded extremes', async ({ page }) => {
  await page.getByRole('button', { name: 'Sent Items' }).first().click()
  await expect(page.getByText('No messages here')).toBeVisible()
  await page.getByRole('button', { name: /^INBOX/ }).click()
  await expect(messageList(page).getByRole('option').first()).toBeVisible()

  // The seed puts the extremes in the first sync window, so local search
  // must find them. Search runs as you type.
  const search = page.getByRole('textbox', { name: 'Search mail' })
  await search.fill(manifest.aliceShortSubject)
  const shortHit = page.getByRole('option', { name: new RegExp(escapeRegExp(manifest.aliceShortSubject)) })
  await expect(shortHit).toBeVisible()
  await shortHit.click()
  await expect.poll(async () => (await readerText(page)).trim()).toBe(manifest.shortBody)

  await search.fill(manifest.aliceLargeSubject)
  const largeHit = page.getByRole('option', { name: new RegExp(escapeRegExp(manifest.aliceLargeSubject)) })
  await expect(largeHit).toBeVisible()
  const t0 = Date.now()
  await largeHit.click()
  await expect(page.locator('section.detail pre.body-plain')).toContainText(manifest.largePrefix, { timeout: 60_000 })
  timings['open large message ms'] = Date.now() - t0

  await search.fill('')
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: /^INBOX/ }).click()
})

test('Alice sends, Bob replies, Alice sees it', async ({ page }) => {
  // Unique per run: a reused Stalwart keeps mail from earlier runs.
  const subject = `e2e alice to bob ${Date.now()}`
  await page.getByRole('button', { name: 'Compose' }).click()
  const to = page.getByRole('textbox', { name: 'To', exact: true })
  await to.fill('bob@example.org')
  // A fresh profile has no contacts yet, so the To suggestion may not appear.
  // Message rows from Bob are options too; only look inside the compose dialog.
  const suggestion = page.getByRole('dialog', { name: 'Compose message' }).getByRole('option', { name: 'bob@example.org' })
  if (await suggestion.isVisible({ timeout: 2_000 }).catch(() => false)) {
    await suggestion.click()
  } else {
    await to.press('Enter')
  }
  await expect(page.getByRole('button', { name: 'Remove bob@example.org' })).toBeVisible()
  await page.getByRole('textbox', { name: 'Subject' }).fill(subject)
  await page.getByRole('dialog', { name: 'Compose message' }).locator('.cm-content').click()
  await page.keyboard.insertText('hello from alice')
  await page.getByRole('button', { name: 'Send', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Compose message' })).toBeHidden({ timeout: 30_000 })

  await page.keyboard.press('ControlOrMeta+m')
  await page.getByRole('button', { name: 'Add a mailbox Sign in to an account by hand' }).click()
  await fillOtherAccount(page, 'bob@example.org', 'bob-e2e')
  // the wizard stays open on the folder picker; nothing syncs until it is answered.
  await page.getByRole('button', { name: 'Sync everything' }).click()
  await page.getByRole('button', { name: 'Done', exact: true }).click()
  await expect(page.getByRole('button', { name: 'bob@example.org' })).toBeVisible({ timeout: 30_000 })
  await accountInbox(page, 'bob@example.org').click()
  const arrived = page.getByRole('option', { name: new RegExp(subject) })
  await expect(arrived).toBeVisible({ timeout: 90_000 })
  await arrived.click()
  await expect.poll(() => readerText(page)).toContain('hello from alice')

  await page.getByRole('button', { name: 'Reply', exact: true }).click()
  await expect(page.getByRole('combobox', { name: 'From' })).toContainText('bob@example.org')
  const editor = page.locator('.cm-content')
  await editor.click()
  await page.keyboard.insertText('hello from bob\n')
  await page.getByRole('button', { name: 'Send', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Compose message' })).toBeHidden({ timeout: 30_000 })

  await accountInbox(page, 'alice@example.org').click()
  await expect(page.getByRole('option', { name: new RegExp(`Re: ${subject}`) })).toBeVisible({ timeout: 90_000 })
  timings['bob server inbox count'] = manifest.bobCount
})
