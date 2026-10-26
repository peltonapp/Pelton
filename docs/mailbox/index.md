---
title: Setting up a mailbox
description: Connect Gmail, iCloud, Outlook, Purelymail, Fastmail, Yahoo, a generic IMAP/SMTP account, or import from Thunderbird.
---

# Setting up a mailbox

To manage your mailboxes, open Settings
(<span class="os-key-mac">++cmd+comma++</span><span class="os-key-other">++ctrl+comma++</span>,
or **Pelton > Settings** from the menu bar) and go to **Accounts** in the sidebar. Accounts can
be edited, added, or removed there.

To add a mailbox faster, press
<span class="os-key-mac">++cmd+m++</span><span class="os-key-other">++ctrl+m++</span>
instead.

Adding a mailbox starts with a choice between setting up a fresh account or
importing one from another client:

![The "Set up your mail" screen: Add a mailbox, or import from another client](../assets/screenshots/screenshot-mailbox-setup-choice.png)

Choosing **Add a mailbox** shows the provider picker:

![The provider picker: Gmail, Outlook/Microsoft 365, iCloud, Yahoo, Fastmail, Purelymail, and Other (IMAP/SMTP)](../assets/screenshots/screenshot-mailbox-provider-picker.png)

Pick your provider:

<div class="grid cards" markdown>

-   __Gmail__

    ---

    [Set up Gmail &rarr;](gmail.md)

-   __iCloud__

    ---

    [Set up iCloud &rarr;](icloud.md)

-   __Outlook__

    ---

    [Set up Outlook &rarr;](outlook.md)

-   __Purelymail__

    ---

    [Set up Purelymail &rarr;](imap-smtp.md)

-   __Fastmail__

    ---

    [Set up Fastmail &rarr;](imap-smtp.md)

-   __Yahoo__

    ---

    [Set up Yahoo &rarr;](imap-smtp.md)

-   __Generic IMAP/SMTP__

    ---

    [Set up a generic account &rarr;](imap-smtp.md)

-   __Importing from Thunderbird__

    ---

    [Import from Thunderbird &rarr;](thunderbird-import.md)

</div>

??? question "Which one do I need?"
    - Using a `@gmail.com`, `@googlemail.com` or Google Workspace address? Use [Gmail](gmail.md).
    - Using an `@icloud.com`, `@me.com`, or `@mac.com` address? Use [iCloud](icloud.md).
    - Using an `@outlook.com`, `@hotmail.com`, or Microsoft 365 address? Use [Outlook](outlook.md).
    - Moving your accounts and mail over from Thunderbird? Use
      [Importing from Thunderbird](thunderbird-import.md).
    - Using Purelymail, Fastmail, or Yahoo Mail? Pick that provider's tile in the picker.
      Pelton pre-fills the IMAP/SMTP settings for you; see
      [Generic IMAP/SMTP](imap-smtp.md#provider-presets) for details.
    - Anything else, self-hosted, or a work server your IT team gave you settings for? Use
      [Generic IMAP/SMTP](imap-smtp.md).

## Sync settings

Three settings in **Settings > Sync & power** control how much Pelton
downloads. **Messages to sync per folder** (default 100) is how many of the
newest messages are kept per folder, both in the list and in full; **All**
keeps every message, newest first. Older mail is fetched from the server
when you reach the end of the list (or with **Load older mail** when
automatic fetching is off), or by raising the limit; opening a listed
message whose body is not downloaded yet fetches it. **Parallel sync
connections** (2 to 10, default 3) is how many connections one mailbox may
open at once, counting every connection, including the one that listens for
new mail; override it for a single mailbox in its editor. If the server
refuses a connection, Pelton lowers the number by itself. New mail does not
wait for the whole background download: with one connection it waits until
the current batch is done, with more it comes in right away. Sending never uses or waits for these connections.
Syncing after a restart only asks the server what changed since last time.
**Full folder check every (days)** (default 7) re-checks each folder in full in
the background when its last full check is older than that, which catches
anything a quick sync missed; **Manual only** leaves it to the Sync button.
The Sync button runs the full check only on folders not checked in the last
24 hours.

Settings > Accounts shows a connection line under each mailbox:
`IMAP · host:port`.

## Need help?

See [Support](../support.md).
