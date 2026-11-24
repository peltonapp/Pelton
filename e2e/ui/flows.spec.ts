import { expect, test, type Locator, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { readFileSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { SYNC_WINDOW, assertE2EApp, countMessages, escapeRegExp, messageList, waitStable } from './helpers'

const here = dirname(fileURLToPath(import.meta.url))
const manifest = JSON.parse(readFileSync(join(here, '../manifest.json'), 'utf8')) as {
  aliceCount: number
  bobCount: number
  aliceSearchSubject: string
  // The UID of each message seeded into Bob's inbox, in append order.
  bobUids: number[] | null
}
// run.sh sets E2E_DB to the e2e app's database (e2e_data_dir in e2e/lib.sh).
const db = process.env.E2E_DB ?? ''
if (!db) {
  throw new Error('E2E_DB is required (the e2e app database; run the suite through e2e/run.sh)')
}
const timings: Record<string, number | string> = {}

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

// Read-only: the app owns the database; the suite only looks.
function sql(query: string): string {
  return execFileSync('sqlite3', ['-readonly', db, query], { encoding: 'utf8' }).trim()
}

// openAccountInbox expands account in the sidebar and opens its inbox. A
// Stalwart account can list more than one inbox-named folder, so it tries
// each until one shows rows.
async function openAccountInbox(page: Page, account: string, waitMs = 10_000) {
  const header = page.getByRole('button', { name: account, exact: true })
  if ((await header.getAttribute('aria-expanded')) !== 'true') {
    await header.click()
  }
  const inboxes = page
    .locator('section.account')
    .filter({ has: header })
    .getByRole('button', { name: /^(INBOX|Inbox)\b/ })
  const count = await inboxes.count()
  if (count === 0) throw new Error(`no inbox under ${account}`)
  const row = messageList(page).getByRole('option').first()
  for (let i = 0; i < count; i++) {
    await inboxes.nth(i).click()
    try {
      await row.waitFor({ state: 'visible', timeout: waitMs })
      return
    } catch {
      // try the next inbox-named folder
    }
  }
  await expect(page.getByText('No messages here')).toBeHidden({ timeout: 1_000 })
  await expect(row).toBeVisible({ timeout: 5_000 })
}

test.beforeEach(async ({ page }) => {
  await page.goto('/')
  await messageList(page).waitFor()
})

// A failed or aborted earlier run can leave sync_message_limit at All in the
// app DB. Reset it through the UI (the app caches settings, so SQL is not safe).
// First of all, PELTON_URL must be the e2e app.
test.beforeAll(async ({ browser }) => {
  await assertE2EApp(browser)
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
  test.setTimeout(300_000)
  let previousTop = ''
  for (const account of ['alice@example.org', 'bob@example.org']) {
    await openAccountInbox(page, account)
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
    await expect.poll(async () => (await rowTexts(list))[0]).not.toBe(top[0])
    const mid = await rowTexts(list)

    const started = Date.now()
    await jump(list, 0.7)
    await expect.poll(async () => (await rowTexts(list))[0]).not.toBe(mid[0])
    timings[`deep scroll ${account} ms`] = Date.now() - started
    expect(await list.getByRole('option').count()).toBeLessThan(80)

    await jump(list, 0)
    // New mail (e.g. Bob's reply from the inbox suite) can land on top meanwhile,
    // and a row gains its preview once its body downloads, so match by prefix.
    await expect
      .poll(async () => (await rowTexts(list)).some((row) => row.startsWith(top[0]) || top[0].startsWith(row)))
      .toBe(true)

    // Settle first: the first-sync window filling up, or a backfill the jumps
    // above started, must not count as this scroll's backfill.
    const before = await waitStable(() => countMessages(page))
    await expect
      .poll(
        async () => {
          await jump(list, 1)
          return countMessages(page)
        },
        { timeout: 30_000, intervals: [600] },
      )
      .toBeGreaterThan(Math.max(before, SYNC_WINDOW))
    const after = await countMessages(page)
    timings[`${account} loaded count`] = `${before} -> ${after}`
    expect(await list.getByRole('option').count()).toBeLessThan(manifest.aliceCount / 10)
  }
})

// sqlJSON runs a read-only query and parses sqlite3's -json output (nothing
// at all for no rows).
function sqlJSON<T>(query: string): T[] {
  const out = execFileSync('sqlite3', ['-readonly', '-json', db, query], { encoding: 'utf8' }).trim()
  return out ? (JSON.parse(out) as T[]) : []
}

function quote(v: string): string {
  return `'${v.replace(/'/g, "''")}'`
}

type FolderRow = { id: number; role_override: string | null; attributes: string | null; imap_path: string }

// The roles a user may assign (assignableRoles in internal/desktop/dto.go):
// an override outside this list is ignored, and inbox is never assignable.
const ASSIGNABLE_ROLES = new Set(['normal', 'sent', 'drafts', 'trash', 'junk', 'archive'])
const ATTRIBUTE_ROLES = new Set(['inbox', 'sent', 'drafts', 'trash', 'junk', 'archive'])
const PATH_ROLES: Record<string, string> = {
  INBOX: 'inbox',
  SENT: 'sent',
  DRAFTS: 'drafts',
  TRASH: 'trash',
  JUNK: 'junk',
  ARCHIVE: 'archive',
}

// folderRole mirrors folderRole in internal/desktop/dto.go: a valid
// role_override, then the first SPECIAL-USE attribute (stored space-separated),
// then the well-known path, else normal. Never the display name.
function folderRole(f: FolderRow): string {
  const override = f.role_override ?? ''
  if (override && ASSIGNABLE_ROLES.has(override)) return override
  for (const attr of (f.attributes ?? '').split(' ')) {
    const role = attr.replace(/^\\/, '').toLowerCase()
    if (ATTRIBUTE_ROLES.has(role)) return role
  }
  return PATH_ROLES[f.imap_path.toUpperCase()] ?? 'normal'
}

// folderRolesOf lists the role of each folder of account that holds a message
// with the RFC Message-ID rfcId, sorted.
function folderRolesOf(email: string, rfcId: string): string[] {
  return sqlJSON<FolderRow>(
    `SELECT f.id, f.role_override, f.attributes, f.imap_path ` +
      `FROM messages m JOIN folders f ON f.id = m.folder_id JOIN accounts a ON a.id = m.account_id ` +
      `WHERE a.email = ${quote(email)} AND m.message_id = ${quote(rfcId)}`,
  )
    .map(folderRole)
    .sort()
}

// inboxFolderIds lists account's folders whose role is inbox. It fails when
// there is none, so a count over them cannot pass by being 0.
function inboxFolderIds(email: string): number[] {
  const ids = sqlJSON<FolderRow>(
    `SELECT f.id, f.role_override, f.attributes, f.imap_path ` +
      `FROM folders f JOIN accounts a ON a.id = f.account_id WHERE a.email = ${quote(email)}`,
  )
    .filter((f) => folderRole(f) === 'inbox')
    .map((f) => Number(f.id))
  if (ids.length === 0) throw new Error(`no inbox-role folder for ${email}`)
  return ids
}

// inboxRows counts the messages stored locally in account's inbox, stubs
// included.
function inboxRows(email: string): number {
  return Number(sql(`SELECT COUNT(*) FROM messages WHERE folder_id IN (${inboxFolderIds(email).join(',')})`))
}

// completeBodies counts the messages in account's inbox whose body is stored.
function completeBodies(email: string): number {
  return Number(
    sql(
      `SELECT COALESCE(SUM(body_complete != 0), 0) FROM messages ` +
        `WHERE folder_id IN (${inboxFolderIds(email).join(',')})`,
    ),
  )
}

// inboxMaxId is the highest row id in account's inbox, 0 when it is empty.
// messages.id is AUTOINCREMENT, so ids are never reused: a row with a higher
// id was inserted after this was read, i.e. it is outside that snapshot.
function inboxMaxId(email: string): number {
  return Number(
    sql(`SELECT COALESCE(MAX(id), 0) FROM messages WHERE folder_id IN (${inboxFolderIds(email).join(',')})`),
  )
}

// completeBodiesAbove counts the messages in account's inbox inserted after
// the row with id floor whose body is stored.
function completeBodiesAbove(email: string, floor: number): number {
  return Number(
    sql(
      `SELECT COUNT(*) FROM messages WHERE body_complete != 0 AND id > ${Math.trunc(floor)} ` +
        `AND folder_id IN (${inboxFolderIds(email).join(',')})`,
    ),
  )
}

// inboxMinUid is the lowest server UID stored in account's inbox, stubs
// included, or 0 when no row has one.
function inboxMinUid(email: string): number {
  return Number(
    sql(
      `SELECT COALESCE(MIN(uid), 0) FROM messages WHERE uid > 0 ` +
        `AND folder_id IN (${inboxFolderIds(email).join(',')})`,
    ),
  )
}

// inboxRowsUpToUid counts the rows in account's inbox with a UID in
// 1..maxUid.
function inboxRowsUpToUid(email: string, maxUid: number): number {
  return Number(
    sql(
      `SELECT COUNT(*) FROM messages WHERE uid > 0 AND uid <= ${Math.trunc(maxUid)} ` +
        `AND folder_id IN (${inboxFolderIds(email).join(',')})`,
    ),
  )
}

// completeBodiesUpToUid counts the messages in account's inbox inserted after
// the row with id floor, with a UID in 1..maxUid, whose body is stored.
function completeBodiesUpToUid(email: string, floor: number, maxUid: number): number {
  return Number(
    sql(
      `SELECT COUNT(*) FROM messages WHERE body_complete != 0 AND id > ${Math.trunc(floor)} ` +
        `AND uid > 0 AND uid <= ${Math.trunc(maxUid)} ` +
        `AND folder_id IN (${inboxFolderIds(email).join(',')})`,
    ),
  )
}

// allOnlyMaxUid is the highest seeded UID in Bob's inbox that no work before
// All can admit, or 0 when there is none.
//
// The window and "load older" admit from the newest UID down: the window is
// the highest UIDs and each page lists just below the folder's floor. So
// before All, Bob's rows are the rowsBefore highest UIDs of his inbox, and
// work still in flight then adds at most pendingRows more below them. A
// seeded message has at least as many messages above it as seeded messages
// with a higher UID that are still in the inbox (new mail lands higher
// still); removed of the seed may have left. It is out of reach when that
// lower bound exceeds rowsBefore + pendingRows. seedUids is in append
// order, which is ascending UID order.
function allOnlyMaxUid(
  seedUids: readonly number[],
  removed: number,
  rowsBefore: number,
  pendingRows: number,
): number {
  const sorted = [...seedUids].sort((a, b) => a - b)
  // sorted[k] has sorted.length - 1 - k seeded UIDs above it, at least
  // sorted.length - 1 - k - removed of them still there.
  const reach = rowsBefore + pendingRows
  const cutoff = sorted.length - Math.max(0, removed) - reach - 1
  return cutoff >= 0 ? sorted[cutoff] : 0
}

async function openSettingsSection(page: Page, name: string) {
  // A previous failure may have left the dialog open; the shortcut would toggle it shut.
  if (!(await page.getByRole('dialog', { name: 'Settings' }).isVisible())) {
    await page.keyboard.press('ControlOrMeta+,')
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
  const parallelBefore = (await parallel.getAttribute('aria-valuetext')) ?? ''
  const slider = page.getByRole('slider', { name: 'Messages to sync per folder' })
  await slider.evaluate((el, i) => {
    const input = el as HTMLInputElement
    input.value = String(i)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  }, index)
  await expect(slider).toHaveAttribute('aria-valuetext', label)
  await expect(parallel).toHaveAttribute('aria-valuetext', parallelBefore)
}

test('forced sync leaves Alice and Bob mail in place', async ({ page }) => {
  const syncNow = page.getByRole('button', { name: 'Sync now' })
  for (const account of ['alice@example.org', 'bob@example.org']) {
    await openAccountInbox(page, account)
    const before = await countMessages(page)
    await syncNow.click()
    // The button spins for the whole manual sync, which lists every UID of a
    // 6,400-message inbox, so it is on screen long enough to catch.
    await expect(syncNow).toHaveClass(/\bspinning\b/)
    await expect(syncNow).not.toHaveClass(/\bspinning\b/, { timeout: 60_000 })
    await expect(messageList(page).getByRole('option').first()).toBeVisible()
    const after = await countMessages(page)
    timings[`sync now ${account} count`] = `${before} -> ${after}`
    expect(after).toBeGreaterThanOrEqual(before)
  }
})

test('search a seeded subject, mark unread, switch folder, archive', async ({ page }) => {
  await openAccountInbox(page, 'alice@example.org')
  // The seed puts the search target in the first sync window. Search runs as you type.
  const search = page.getByRole('textbox', { name: 'Search mail' })
  await search.fill(manifest.aliceSearchSubject)
  await expect(page.getByRole('option', { name: new RegExp(escapeRegExp(manifest.aliceSearchSubject)) })).toBeVisible()
  await search.fill('')
  await page.keyboard.press('Escape')
  await openAccountInbox(page, 'alice@example.org')

  // Pin the row by message id: the list re-renders as rows change state.
  const third = messageList(page).getByRole('option').nth(2)
  const wrapper = await third.evaluate((el) => el.closest('[id^="msg-"]')?.id ?? '')
  expect(wrapper).not.toBe('')
  const row = page.locator(`#${wrapper} [role="option"]`)
  // Opening marks it read; Mark as unread must flip it back while it stays open.
  await row.click()
  await expect(row).not.toHaveClass(/\bunread\b/)
  await row.click({ button: 'right' })
  await page.getByRole('menuitem', { name: 'Mark as unread' }).click()
  await expect(row).toHaveClass(/\bunread\b/)

  await page.getByRole('button', { name: 'Sent Items' }).first().click()
  await openAccountInbox(page, 'alice@example.org')

  // Pin the victim by its message id (msg-<id> is the local messages.id) and
  // remember its Message-ID: the server move gives it a new UID, so the copy
  // in Archive comes back as a new row.
  const victimWrapper = await messageList(page)
    .getByRole('option')
    .nth(4)
    .evaluate((el) => el.closest('[id^="msg-"]')?.id ?? '')
  const victimId = Number(victimWrapper.replace(/^msg-/, ''))
  expect(victimId).toBeGreaterThan(0)
  const rfcId = sql(`SELECT message_id FROM messages WHERE id = ${victimId}`)
  expect(rfcId).not.toBe('')
  const victim = page.locator(`#${victimWrapper}`)
  await victim.getByRole('option').click()
  // The Unified "Archive" view is also named Archive when its count is empty.
  await page.locator('button[aria-label="Archive"]').click()
  await expect(victim).toHaveCount(0)
  expect(sql(`SELECT COUNT(*) FROM messages WHERE id = ${victimId}`)).toBe('0')
  // The next sync fetches it under Alice's archive-role folder.
  await page.getByRole('button', { name: 'Sync now' }).click()
  await expect
    .poll(() => folderRolesOf('alice@example.org', rfcId), { timeout: 90_000, intervals: [2_000] })
    .toEqual(['archive'])
})

test('two compose windows while a message is open, and switch accounts', async ({ page }) => {
  await openAccountInbox(page, 'alice@example.org')
  await messageList(page).getByRole('option').first().click()
  await expect(page.locator('section.detail')).toBeVisible()
  const compose = page.getByRole('dialog', { name: 'Compose message' })
  // exact: "Close compose" would match too once a window is open.
  const newCompose = page.getByRole('button', { name: 'Compose', exact: true })
  await newCompose.click()
  await expect(compose).toHaveCount(1)
  await newCompose.click()
  await expect(compose).toHaveCount(2)
  // Both are empty, so closing asks nothing.
  await compose.first().getByRole('button', { name: 'Close compose' }).click()
  await expect(compose).toHaveCount(1)
  await compose.getByRole('button', { name: 'Close compose' }).click()
  await expect(compose).toHaveCount(0)
  await expect(page.locator('section.detail')).toBeVisible()

  // The account headers toggle; openAccountInbox expands only when collapsed.
  await openAccountInbox(page, 'bob@example.org')
  await expect(messageList(page).getByRole('option').first()).toContainText('alice@example.org')
  await openAccountInbox(page, 'alice@example.org')
  await expect(messageList(page).getByRole('option').first()).toContainText('bob@example.org')
})

test('select all on Alice selects the loaded page, not only the DOM rows', async ({ page }) => {
  await openAccountInbox(page, 'alice@example.org')
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
  await openAccountInbox(page, 'bob@example.org')
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

// Last on purpose: All enqueues every older page of every folder, and setting
// the limit back to 100 does not cancel pages already queued, so any test after
// this one would run under the rest of the ~500 MB corpus downloading.
// The All test's waits, and its time limit derived from them so the two cannot
// drift apart: the settings and sidebar steps get the margin.
const ALL_WAITS = {
  inboxSettle: 240_000,
  listWholeInbox: 180_000,
  bodiesPastSnapshot: 150_000,
}
const ALL_MARGIN = 120_000
// EARLIER_PAGES_IN_FLIGHT is how many backfills begun before All may still
// add rows after the snapshot, each at most one limit (backfillBatch, the
// 100 set below) of messages: a "load older" call whose page had not landed
// yet, the running normal-lane `older:<folder>` chain, and one queued behind
// it (the scheduler merges any further Enqueue of that key into the queued
// task). Nothing in this test scrolls Bob's list, so it starts no backfill.
const EARLIER_PAGES_IN_FLIGHT = 3

test('All on Messages to sync per folder admits the whole inbox and fetches bodies for rows only All listed', async ({ page }) => {
  test.setTimeout(Object.values(ALL_WAITS).reduce((a, b) => a + b, 0) + ALL_MARGIN)
  const bob = 'bob@example.org'
  // The seed's APPENDUIDs say which messages lie deeper than any earlier
  // backfill reaches; a manifest from an older seed has none.
  const seedUids = manifest.bobUids ?? []
  expect(seedUids).toHaveLength(manifest.bobCount)
  await openSettingsSection(page, 'Sync & power')
  await setMessageLimit(page, 1, '100')
  await closeSettings(page)
  await openAccountInbox(page, bob)
  // Earlier backfills (the scroll test) may still be listing a page or
  // downloading its bodies. Wait until rows and complete bodies both hold
  // still together, so listing and body work are idle and only All moves them.
  await waitStable(() => `${inboxRows(bob)}/${completeBodies(bob)}`, { timeoutMs: ALL_WAITS.inboxSettle })
  // Snapshot right before All: every row that exists now is excluded below.
  const rowsBefore = inboxRows(bob)
  const bodiesBefore = completeBodies(bob)
  const maxIdBefore = inboxMaxId(bob)
  const minUidBefore = inboxMinUid(bob)
  expect(rowsBefore).toBeLessThan(manifest.bobCount)
  expect(minUidBefore).toBeGreaterThan(0)
  try {
    await openSettingsSection(page, 'Sync & power')
    const started = Date.now()
    await setMessageLimit(page, 5, 'All')
    await closeSettings(page)
    await page.getByRole('button', { name: 'Sync now' }).click()
    // Only All lists the rest of the inbox: a backfill page or the 100 window
    // adds at most 100 rows, the seed put bobCount in Bob's inbox.
    await expect
      .poll(() => inboxRows(bob), { timeout: ALL_WAITS.listWholeInbox, intervals: [2_000] })
      .toBeGreaterThanOrEqual(manifest.bobCount)
    timings['All listed whole inbox ms'] = Date.now() - started
    // A row id above the snapshot does not say who listed the row: a page an
    // earlier backfill still had in flight inserts above it too. Count only
    // seeded messages deeper than everything before All can reach: the rows
    // at the snapshot plus EARLIER_PAGES_IN_FLIGHT pages of the 100 limit.
    // Seeded rows missing now (the whole inbox is listed) left the inbox, and
    // each lets the window reach one deeper. Overcounting them (All may still
    // be listing) only lowers the cutoff.
    const maxSeedUid = Math.max(...seedUids)
    const removed = Math.max(0, manifest.bobCount - inboxRowsUpToUid(bob, maxSeedUid))
    const allOnlyUid = allOnlyMaxUid(seedUids, removed, rowsBefore, EARLIER_PAGES_IN_FLIGHT * SYNC_WINDOW)
    // At least one seeded message is out of reach, and the snapshot agrees:
    // no row stored before All is that deep, else the model above is wrong.
    expect(allOnlyUid).toBeGreaterThan(0)
    expect(minUidBefore).toBeGreaterThan(allOnlyUid)
    // A complete body on such a row is a body for a message only All listed.
    await expect
      .poll(() => completeBodiesUpToUid(bob, maxIdBefore, allOnlyUid), {
        timeout: ALL_WAITS.bodiesPastSnapshot,
        intervals: [1_000],
      })
      .toBeGreaterThan(0)
    timings['rows before and after All'] = `${rowsBefore} -> ${inboxRows(bob)}`
    timings['bodies before All'] = bodiesBefore
    timings['bodies after All'] = completeBodies(bob)
    timings['bodies on rows inserted after the snapshot'] = completeBodiesAbove(bob, maxIdBefore)
    timings['seeded UIDs only All can admit'] = `1..${allOnlyUid} (${removed} seeded left the inbox)`
    timings['bodies on rows only All can admit'] = completeBodiesUpToUid(bob, maxIdBefore, allOnlyUid)
    timings['All fetched a body past the snapshot ms'] = Date.now() - started
  } finally {
    // Always restore the default so a failure cannot leave a later run at All.
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
