// demo.ts holds the fixed, potato-themed sample data for the cosmetic demo mode
// (the --potatoes-are-nice flag). None of it touches the backend: when demo mode
// is active the api layer returns this data instead of calling the store, so a
// website screenshot shows a full, friendly inbox without any real account. It is
// 100% cosmetic and never persists anything.

import type {
  Account,
  Folder,
  UnifiedView,
  MessageList,
  MessageSummary,
  MessageDetail,
  OutboxRow,
} from './types'
import { blankAccountProxy } from './proxyroute'

// demoActive is set once at startup from the backend IsDemoMode() flag.
let demoActive = false

/** setDemoActive records whether the app launched in demo mode. */
export function setDemoActive(value: boolean): void {
  demoActive = value
}

/** isDemoActive reports whether the api layer should serve sample data. */
export function isDemoActive(): boolean {
  return demoActive
}

// relative timestamps computed once so the list reads like a live inbox.
const now = Date.now()
const ago = (mins: number): string => new Date(now - mins * 60_000).toISOString()

// the two sample mailboxes, both Pelton/potato themed.
const accounts: Account[] = [
  {
    id: 1,
    email: 'spud@pelton.email',
    displayName: 'Spud McPelton',
    localLabel: '',
    useLocalLabel: false,
    username: '',
    imapHost: 'imap.pelton.email',
    imapPort: 993,
    smtpHost: 'smtp.pelton.email',
    smtpPort: 465,
    local: false,
    imapTls: 'ssl',
    smtpTls: 'ssl',
    exportOnArchive: false,
    exportDir: '',
    exportSubfolders: 'none',
    exportNameTemplate: '',
    pgpDefault: '',
    passwordPromptDismissed: false,
    trustedCerts: [],
    caSubjects: [],
    proxy: blankAccountProxy(),
  },
  {
    id: 2,
    email: 'harvest@pelton-potato.island',
    displayName: 'Potato Island Co-op',
    localLabel: '',
    useLocalLabel: false,
    username: '',
    imapHost: 'imap.pelton-potato.island',
    imapPort: 993,
    smtpHost: 'smtp.pelton-potato.island',
    smtpPort: 465,
    local: false,
    imapTls: 'ssl',
    smtpTls: 'ssl',
    exportOnArchive: false,
    exportDir: '',
    exportSubfolders: 'none',
    exportNameTemplate: '',
    pgpDefault: '',
    passwordPromptDismissed: false,
    trustedCerts: [],
    caSubjects: [],
    proxy: blankAccountProxy(),
  },
]

function foldersFor(accountId: number, inboxUnread: number, inboxTotal: number): Folder[] {
  const f = (id: number, name: string, role: string, unread: number, total: number): Folder => ({
    id,
    accountId,
    name,
    imapPath: name,
    delimiter: '/',
    parentId: null,
    syncExcluded: false,
    role,
    unreadCount: unread,
    totalCount: total,
    attributes: [],
    pinned: false,
    // the demo folders carry their role directly, so none of them is an
    // override of anything.
    roleOverride: '',
  })
  const base = accountId * 100
  return [
    f(base + 1, 'Inbox', 'inbox', inboxUnread, inboxTotal),
    f(base + 2, 'Sent', 'sent', 0, 128),
    f(base + 3, 'Drafts', 'drafts', 0, 2),
    f(base + 4, 'Archive', 'archive', 0, 512),
    f(base + 5, 'Spam', 'junk', 1, 9),
    f(base + 6, 'Trash', 'trash', 0, 24),
  ]
}

const foldersByAccount: Record<number, Folder[]> = {
  1: foldersFor(1, 6, 42),
  2: foldersFor(2, 2, 21),
}

const views: UnifiedView[] = [
  { key: 'inbox', label: 'Unified Inbox', unreadCount: 8, totalCount: 63 },
  { key: 'flagged', label: 'Flagged', unreadCount: 0, totalCount: 2 },
  { key: 'drafts', label: 'Drafts', unreadCount: 0, totalCount: 3 },
  { key: 'sent', label: 'Sent', unreadCount: 0, totalCount: 256 },
  { key: 'archive', label: 'Archive', unreadCount: 0, totalCount: 1024 },
  { key: 'junk', label: 'Spam', unreadCount: 1, totalCount: 18 },
  { key: 'trash', label: 'Trash', unreadCount: 0, totalCount: 48 },
]

// one summary builder so every row is consistent.
function msg(
  id: number,
  fromName: string,
  fromAddress: string,
  subject: string,
  snippet: string,
  minsAgo: number,
  opts: Partial<MessageSummary> = {},
): MessageSummary {
  return {
    id,
    accountId: 1,
    folderId: 101,
    accountEmail: 'spud@pelton.email',
    folderName: 'Inbox',
    subject,
    fromName,
    fromAddress,
    snippet,
    date: ago(minsAgo),
    seen: true,
    flagged: false,
    hasAttachments: false,
    pgp: '',
    auth: '',
    flagColor: 0,
    offline: false,
    snoozeUntil: '',
    senderVip: false,
    // the demo mail is not signed, so there is no verdict to show.
    smime: { status: '', signer: '', email: '', issuer: '', detail: '' },
    ...opts,
  }
}

const messages: MessageSummary[] = [
  msg(1, 'Marina Tuber', 'marina@pelton-potato.island', 'Q4 potato numbers', 'Here are the Q4 yield figures for the russet fields, up 12% on last quarter. Take a look before the board call.', 14, { seen: false, flagged: true, hasAttachments: true, flagColor: 1 }),
  msg(2, 'Pelton Potato Island', 'hello@pelton-potato.island', 'Welcome to Potato Island 🥔', 'Thanks for joining the co-op! Here is everything you need to know about this season’s harvest, storage and market days.', 52, { seen: false }),
  msg(3, 'Chip Mash', 'chip@fryco.potato', 'Re: Fryer maintenance schedule', 'Sounds good, let’s do Tuesday. I’ll bring the oil samples and the new baskets.', 96, {}),
  msg(4, 'Yukon Gold', 'billing@goldseed.potato', 'Invoice #4048 — seed potatoes', 'Your order of 4,048 sacks of certified seed potatoes is confirmed and ready for pickup.', 130, { seen: false, hasAttachments: true }),
  msg(5, 'Marina Tuber', 'marina@pelton-potato.island', 'Harvest festival — potato salad?', 'Are you bringing potato salad on Friday? I’ll handle the mash and the gravy boat.', 200, { flagged: true, flagColor: 3 }),
  msg(6, 'Spud Weekly', 'news@spudweekly.potato', 'This week in potatoes', 'Blight watch, market prices, and a loving profile of the humble fingerling.', 320, {}),
  msg(7, 'Dr. Solanum', 'research@tuberlab.potato', 'New blight-resistant variety', 'We’ve had very promising results with the Pelton-7 cultivar this season. Details inside.', 540, {}),
  msg(8, 'Rosa Fingerling', 'rosa@peltonfarms.potato', 'North field irrigation is in', 'The drip lines are installed. Soil moisture looks perfect for the earlies.', 900, {}),
  msg(9, 'Couch Potato', 'relax@sofa.potato', 'Movie night?', 'Bringing chips (the potato kind). You in for Friday after the festival?', 1500, {}),
  msg(10, 'Pelton HR', 'people@pelton.email', 'Your potato-leave request', 'Approved! Enjoy the long weekend at the spud festival. Bring us back some fries.', 2600, {}),
]

// the shared casual "coworker about potatoes" body every message opens to.
const sharedBodyHtml = `
<div style="font-family: system-ui, sans-serif; font-size: 14px; line-height: 1.6; color: inherit;">
  <p>Hey,</p>
  <p>Quick one before standup — the potato shipment from the north field came in this morning and it is looking <strong>great</strong>. Marina says the Q4 russet numbers are up 12% on last quarter. \u{1F954}</p>
  <p>Could you double-check the crate counts for <strong>Pelton Potato Island</strong> before we send the invoice? I think we are at about <strong>4,048 sacks</strong>, but the paperwork says 4,032.</p>
  <p>Also — potato salad for the harvest festival on Friday? I’ll bring the spuds if you handle the dressing.</p>
  <p>Cheers,<br />Marina</p>
  <p style="color: #999; font-size: 12px;">Pelton Potato Island · Grown with love, mostly underground</p>
</div>`.trim()

/** demoSidebar returns the sample accounts, their folders and the unified views. */
export function demoAccounts(): Account[] {
  return accounts
}

export function demoFolders(accountId: number): Folder[] {
  return foldersByAccount[accountId] ?? []
}

export function demoViews(): UnifiedView[] {
  return views
}

/** demoList returns the sample inbox for any folder or view selection. */
export function demoList(): MessageList {
  return { messages, total: 63, hasOlder: false }
}

/** demoMessage returns the shared potato body wrapped as the given message. */
export function demoMessage(id: number): MessageDetail {
  const summary = messages.find((m) => m.id === id) ?? messages[0]
  return {
    ...summary,
    seen: true,
    toAddresses: 'spud@pelton.email',
    ccAddresses: '',
    bodyPlain: 'Hey, quick one about the potato shipment...',
    bodyQuote: 'Hey, quick one about the potato shipment...',
    bodyHtmlSafe: sharedBodyHtml,
    phishing: { level: 'none' },
    unsubscribe: null,
    // the demo mail says what encoding it uses, so nothing was guessed.
    charsetGuess: '',
    // the demo mail is not protected, so there is nothing to decrypt.
    pgpState: '',
    bodyComplete: true,
    isHtml: true,
    hasRemoteContent: false,
    remoteAllowed: true,
    remoteHosts: [],
    trackingPixels: [],
    attachments: summary.hasAttachments
      ? [{ id: id * 10, filename: 'q4-potato-yields.xlsx', contentType: 'application/vnd.ms-excel', sizeBytes: 48213, inline: false }]
      : [],
  }
}

/** demoSearch returns the sample inbox as a result page, so searching in demo
 * mode never reaches the real mailbox. */
export function demoSearch(): { messages: MessageSummary[]; total: number } {
  return { messages, total: messages.length }
}

/** demoHtml returns the sample body, for the paths that want the raw html of a
 * message rather than the whole detail. */
export function demoHtml(): string {
  return sharedBodyHtml
}

/** demoSource renders a sample message as rfc 822 text, so the source viewer
 * shows the potato mail rather than fetching a real one over imap. */
export function demoSource(id: number): string {
  const m = messages.find((x) => x.id === id) ?? messages[0]
  return [
    `Return-Path: <${m.fromAddress}>`,
    `Delivered-To: spud@pelton.email`,
    `Date: ${new Date(m.date).toUTCString()}`,
    `From: ${m.fromName} <${m.fromAddress}>`,
    `To: Spud McPelton <spud@pelton.email>`,
    `Subject: ${m.subject}`,
    `Message-ID: <${m.id}.potato@pelton.email>`,
    `MIME-Version: 1.0`,
    `Content-Type: text/html; charset=utf-8`,
    `Content-Transfer-Encoding: 8bit`,
    '',
    sharedBodyHtml,
  ].join('\n')
}

/** demoOutbox returns a single message frozen in the "sending" state, so the
 * status bar shows an email on its way out. */
export function demoOutbox(): OutboxRow[] {
  return [
    {
      id: 9001,
      accountId: 1,
      recipients: ['marina@pelton-potato.island'],
      state: 'sending',
      attempts: 1,
      lastError: '',
      nextAttemptAt: '',
      createdAt: ago(0),
    },
  ]
}
