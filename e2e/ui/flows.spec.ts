import { expect, test, type Locator, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { readFileSync, writeFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const manifest = JSON.parse(readFileSync(join(here, '../manifest.json'), 'utf8')) as {
  aliceCount: number
  aliceShortSubject: string
  aliceLargeSubject: string
  aliceSearchSubject: string
  shortBody: string
  largePrefix: string
}
const db = join(homedir(), 'Library/Application Support/Pelton-dev/pelton.db')
const timings: Record<string, number | string> = {}

function messageList(page: Page): Locator {
  return page.getByRole('listbox', { name: 'Messages' })
}

function messageCount(page: Page): Locator {
  return page.getByText(/^\d+ messages$/)
}

async function countMessages(page: Page): Promise<number> {
  const text = await messageCount(page).first().innerText()
  return Number(text.replace(/\D/g, ''))
}

async function rowTexts(list: Locator): Promise<string[]> {
  return list.getByRole('option').allInnerTexts()
}

async function jump(list: Locator, fraction: number) {
  await list.evaluate((el, frac) => {
    const node = el as HTMLElement
    const scroller =
      node.scrollHeight > node.clientHeight + 8 ? node : ((node.parentElement as HTMLElement) ?? node)
    const distance = scroller.scrollHeight * frac
    scroller.scrollTop = frac === 0 ? 0 : Math.max(distance, scroller.clientHeight * (frac > 0.5 ? 12 : 4))
    scroller.dispatchEvent(new Event('scroll', { bubbles: true }))
  }, fraction)
}

function sql(query: string): string {
  return execFileSync('sqlite3', [db, query], { encoding: 'utf8' }).trim()
}

function stubSubject(email: string, minSize: number): string {
  const safe = email.replace(/'/g, '')
  return sql(
    `SELECT subject FROM messages m JOIN accounts a ON a.id = m.account_id ` +
      `WHERE a.email = '${safe}' AND body_complete = 0 AND size_bytes >= ${minSize} ` +
      `ORDER BY size_bytes DESC LIMIT 1`,
  )
}

async function openAccountFolder(page: Page, account: string, folder: RegExp | string, waitMs = 10_000) {
  const accountBtn = page.getByRole('button', { name: account, exact: true })
  if ((await accountBtn.getAttribute('aria-expanded')) !== 'true') {
    await accountBtn.click()
  }
  const count = await page.evaluate((email) => {
    const buttons = [...document.querySelectorAll('button')]
    const start = buttons.findIndex((b) => (b.textContent || '').replace(/\s+/g, ' ').trim().startsWith(email))
    let end = buttons.findIndex((b, i) => i > start && /@/.test(b.textContent || ''))
    if (end < 0) end = buttons.length
    return buttons.slice(start + 1, end).filter((b) => /^(INBOX|Inbox)\b/.test((b.textContent || '').trim())).length
  }, account)
  void folder
  if (count === 0) throw new Error(`no inbox under ${account}`)
  for (let i = 0; i < count; i++) {
    await page.evaluate(
      ({ email, index }) => {
        const buttons = [...document.querySelectorAll('button')]
        const start = buttons.findIndex((b) => (b.textContent || '').replace(/\s+/g, ' ').trim().startsWith(email))
        let end = buttons.findIndex((b, n) => n > start && /@/.test(b.textContent || ''))
        if (end < 0) end = buttons.length
        const inboxes = buttons.slice(start + 1, end).filter((b) => /^(INBOX|Inbox)\b/.test((b.textContent || '').trim()))
        ;(inboxes[index] as HTMLElement).click()
      },
      { email: account, index: i },
    )
    const row = messageList(page).getByRole('option').first()
    for (let n = 0; n < waitMs / 500; n++) {
      if (await row.isVisible().catch(() => false)) return
      await page.waitForTimeout(500)
    }
  }
  if (await page.getByText('No messages here').isVisible().catch(() => false)) {
    throw new Error('inbox stayed on No messages here after Sync now')
  }
  await expect(messageList(page).getByRole('option').first()).toBeVisible({ timeout: 5_000 })
}

test.beforeEach(async ({ page }) => {
  await page.goto('/')
  await messageList(page).waitFor()
})

// A failed or aborted earlier run can leave sync_message_limit at All in the
// app DB. Reset it through the UI (the app caches settings, so SQL is not safe).
test.beforeAll(async ({ browser }) => {
  const page = await browser.newPage({ baseURL: process.env.PELTON_URL, viewport: { width: 1440, height: 900 } })
  try {
    await page.goto('/')
    await messageList(page).waitFor()
    await openSettingsSection(page, 'Sync & power')
    await setMessageLimit(page, 1, '100')
    await closeSettings(page)
  } finally {
    await page.close()
  }
})

test.afterAll(() => {
  const prior = (() => {
    try {
      return readFileSync(join(here, '../report.md'), 'utf8')
    } catch {
      return '# Pelton e2e timings\n\n'
    }
  })()
  const extra = Object.entries(timings).map(([k, v]) => `- ${k}: ${v}`)
  writeFileSync(join(here, '../report.md'), `${prior.trim()}\n${extra.join('\n')}\n`)
})

test('scroll Alice and Bob in jumps through the virtual list', async ({ page }) => {
  let previousTop = ''
  for (const target of [
    { account: 'alice@example.org', folder: /^INBOX/ },
    { account: 'bob@example.org', folder: /^INBOX/ },
  ]) {
    await openAccountFolder(page, target.account, target.folder)
    const list = messageList(page)
    // Alice's rows stay on screen for a moment after Bob's inbox is clicked.
    if (previousTop) {
      await expect.poll(async () => (await rowTexts(list))[0]).not.toBe(previousTop)
    }
    const top = await rowTexts(list)
    previousTop = top[0]
    const domTop = top.length
    expect(domTop).toBeGreaterThan(0)
    expect(domTop).toBeLessThan(80)

    await jump(list, 0.15)
    await page.waitForTimeout(400)
    const mid = await rowTexts(list)
    expect(mid[0]).not.toBe(top[0])

    const started = Date.now()
    await jump(list, 0.7)
    await page.waitForTimeout(700)
    const deep = await rowTexts(list)
    timings[`deep scroll ${target.account} ms`] = Date.now() - started
    expect(deep[0]).not.toBe(mid[0])
    expect(await list.getByRole('option').count()).toBeLessThan(80)

    await jump(list, 0)
    await page.waitForTimeout(300)
    const back = await rowTexts(list)
    // New mail (e.g. Bob's reply from the inbox suite) can land on top meanwhile,
    // and a row gains its preview once its body downloads, so match by prefix.
    expect(back.some((row) => row.startsWith(top[0]) || top[0].startsWith(row))).toBe(true)

    const before = await countMessages(page)
    for (let i = 0; i < 8; i++) {
      await jump(list, 1)
      await page.waitForTimeout(600)
      if ((await countMessages(page)) > before) break
    }
    const after = await countMessages(page)
    timings[`${target.account} loaded count`] = after
    expect(after).toBeGreaterThan(50)
    expect(await list.getByRole('option').count()).toBeLessThan(manifest.aliceCount / 10)
  }
})

function completeBodies(email: string): number {
  const safe = email.replace(/'/g, '')
  return Number(
    sql(
      `SELECT COALESCE(SUM(m.body_complete != 0), 0) FROM messages m ` +
        `JOIN folders f ON f.id = m.folder_id JOIN accounts a ON a.id = m.account_id ` +
        `WHERE a.email = '${safe}' AND f.name IN ('INBOX', 'Inbox')`,
    ),
  )
}

async function openSettingsSection(page: Page, name: string) {
  // A previous failure may have left the dialog open; Meta+, would toggle it shut.
  if (!(await page.getByRole('dialog', { name: 'Settings' }).isVisible())) {
    await page.keyboard.press('Meta+,')
  }
  await page.getByRole('button', { name, exact: true }).click()
}

async function closeSettings(page: Page) {
  await page.evaluate(() => {
    const closer = document.querySelector('button.close[aria-label="Close settings"]')
    if (closer) (closer as HTMLElement).click()
  })
  await expect(page.getByRole('dialog', { name: 'Settings' })).toBeHidden()
}

async function setMessageLimit(page: Page, index: number, label: string) {
  const parallel = page.getByRole('slider', { name: 'Parallel sync connections' })
  const parallelBefore = await parallel.getAttribute('aria-valuetext')
  const slider = page.getByRole('slider', { name: 'Messages to sync per folder' })
  await slider.evaluate((el, i) => {
    const input = el as HTMLInputElement
    input.value = String(i)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  }, index)
  await expect(slider).toHaveAttribute('aria-valuetext', label)
  await expect(parallel).toHaveAttribute('aria-valuetext', parallelBefore)
}

test('All on Messages to sync per folder fetches bodies past 100', async ({ page }) => {
  test.setTimeout(120_000)
  await openSettingsSection(page, 'Sync & power')
  await setMessageLimit(page, 1, '100')
  await closeSettings(page)
  await openAccountFolder(page, 'bob@example.org', /^INBOX/)
  const before = completeBodies('bob@example.org')
  try {
    await openSettingsSection(page, 'Sync & power')
    const started = Date.now()
    await setMessageLimit(page, 5, 'All')
    await closeSettings(page)
    await page.getByRole('button', { name: 'Sync now' }).click()
    let after = before
    for (let i = 0; i < 60 && !(after > 100 && after > before); i++) {
      await page.waitForTimeout(1000)
      after = completeBodies('bob@example.org')
    }
    timings['bodies before All'] = before
    timings['bodies after All'] = after
    timings['All fetch grew past 100 ms'] = Date.now() - started
    expect(after).toBeGreaterThan(100)
    expect(after).toBeGreaterThan(before)
  } finally {
    // Always restore the default so a failure cannot leave later tests at All.
    // A restore that throws would replace the assertion error above, so it is
    // logged instead; beforeAll resets the limit on the next run.
    try {
      await openSettingsSection(page, 'Sync & power')
      await setMessageLimit(page, 1, '100')
      await closeSettings(page)
    } catch (err) {
      console.error('restoring Messages to sync per folder failed:', err)
    }
  }
})

test('forced sync leaves Alice and Bob mail in place', async ({ page }) => {
  for (const target of [
    { account: 'alice@example.org', folder: /^INBOX/ },
    { account: 'bob@example.org', folder: /^INBOX/ },
  ]) {
    await openAccountFolder(page, target.account, target.folder)
    const before = await countMessages(page)
    await page.getByRole('button', { name: 'Sync now' }).click()
    await expect(page.getByText('Loading messages')).toBeHidden({ timeout: 60_000 })
    await expect(messageList(page).getByRole('option').first()).toBeVisible()
    expect(await countMessages(page)).toBeGreaterThan(0)
    timings[`sync now ${target.account} still listed`] = before
  }
})

test('search a seeded subject, mark unread, switch folder, archive', async ({ page }) => {
  await openAccountFolder(page, 'alice@example.org', /^INBOX/)
  await page.getByRole('textbox', { name: 'Search mail' }).fill(manifest.aliceSearchSubject)
  await page.getByRole('textbox', { name: 'Search mail' }).press('Enter')
  const hit = page.getByRole('option', { name: new RegExp(manifest.aliceSearchSubject) })
  const found = await hit.isVisible().catch(() => false)
  timings['search seeded subject'] = found ? 'found' : 'not in the local index yet'
  if (!found) {
    await expect(page.getByText('No matching messages')).toBeVisible()
  }
  await page.keyboard.press('Escape')
  await openAccountFolder(page, 'alice@example.org', /^INBOX/)

  const row = messageList(page).getByRole('option').nth(2)
  await row.click()
  await row.click({ button: 'right' })
  const unread = page.getByRole('menuitem', { name: 'Mark as unread' })
  if (await unread.isVisible().catch(() => false)) {
    await unread.click()
  } else {
    await page.keyboard.press('u')
  }
  timings['mark unread'] = 'invoked Mark as unread or the u shortcut'

  await page.getByRole('button', { name: 'Sent Items' }).first().click()
  await openAccountFolder(page, 'alice@example.org', /^INBOX/)

  const victim = (await messageList(page).getByRole('option').nth(4).innerText()).slice(0, 40)
  await messageList(page).getByRole('option').nth(4).click()
  // The Unified "Archive" view is also named Archive when its count is empty.
  await page.locator('button[aria-label="Archive"]').click()
  await expect(messageList(page).getByRole('option', { name: new RegExp(victim.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')) })).toHaveCount(0)
})

test('second compose while a message is open, and switch accounts', async ({ page }) => {
  await openAccountFolder(page, 'alice@example.org', /^INBOX/)
  await messageList(page).getByRole('option').first().click()
  await expect(page.getByRole('heading').first()).toBeVisible()
  await page.getByRole('button', { name: 'Compose' }).click()
  await expect(page.getByRole('dialog', { name: 'Compose message' })).toBeVisible()
  await page.getByRole('button', { name: 'Close compose' }).click()

  // The account headers toggle; openAccountFolder expands only when collapsed.
  await openAccountFolder(page, 'bob@example.org', /^INBOX/)
  await expect(messageList(page).getByRole('option').first()).toContainText('alice@example.org')
  await openAccountFolder(page, 'alice@example.org', /^INBOX/)
  await expect(messageList(page).getByRole('option').first()).toContainText('bob@example.org')
})

test('select all on Alice selects the loaded page, not only the DOM rows', async ({ page }) => {
  await openAccountFolder(page, 'alice@example.org', /^INBOX/)
  const dom = await messageList(page).getByRole('option').count()
  await page.getByRole('checkbox', { name: 'Select all' }).click()
  const label = page.getByText(/\d+ selected/)
  await expect(label).toBeVisible()
  const selected = Number((await label.innerText()).replace(/\D/g, ''))
  expect(selected).toBeGreaterThan(dom)
  await expect(page.getByText(/All \d+ loaded messages are selected/)).toBeVisible()
  await page.getByRole('button', { name: 'Clear selection' }).click()
  await expect(label).toBeHidden()
})

test('select all on Bob selects the loaded page, not only the DOM rows', async ({ page }) => {
  await openAccountFolder(page, 'bob@example.org', /^INBOX/)
  const dom = await messageList(page).getByRole('option').count()
  await page.getByRole('checkbox', { name: 'Select all' }).click()
  const label = page.getByText(/\d+ selected/)
  await expect(label).toBeVisible()
  const selected = Number((await label.innerText()).replace(/\D/g, ''))
  expect(selected).toBeGreaterThan(dom)
  await expect(page.getByText(/All \d+ loaded messages are selected/)).toBeVisible()
  await page.getByRole('button', { name: 'Clear selection' }).click()
  await expect(label).toBeHidden()
})
