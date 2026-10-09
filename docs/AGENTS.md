# Pelton, machine-readable documentation

Pelton is a free, open source, privacy-focused email client for macOS, Windows and Linux. Go + Wails backend, Svelte frontend. No telemetry. Website: https://pelton.app. Source: https://github.com/peltonapp/Pelton. Human docs: https://docs.pelton.app.

## Install

- macOS 13 or newer: .dmg per release at https://github.com/peltonapp/Pelton/releases (unsigned; right-click Open or `xattr -cr /Applications/Pelton.app`)
- Windows: `Pelton-<version>-windows-amd64-installer.exe` from releases (unsigned)
- Fedora: `sudo dnf copr enable arnek/Pelton && sudo dnf install pelton`
- Raw rpm: `Pelton-<version>-linux-fedora-x86_64.rpm` from releases
- Source: Go + pnpm + wails CLI v2.13.0; `wails build`; on Linux `-tags webkit2_41` with gtk3-devel and webkit2gtk4.1-devel

## Data locations

- macOS: `~/Library/Application Support/Pelton`
- Linux: `~/.config/Pelton`
- Windows: `%AppData%\Pelton`
- Contents: `pelton.db` (settings + mail cache), search index, `themes/<id>/` (installed themes), `logs/` (only if logging is enabled)
- Credentials: OS keyring only (Keychain / Credential Manager / Secret Service)
- Backup: Settings > Import/Export, JSON export, credentials optionally included encrypted (AES-256-GCM, scrypt)

## Network behavior

Only: user's IMAP/SMTP servers; GitHub releases API if update check enabled (default off); BIMI/Gravatar avatar lookups unless avatar source set to generated; remote mail images only after user approval (default blocked); VirusTotal lookups if the user enables the integration and supplies an API key (default off). No telemetry.

## Tracking pixel detection

Off by default, Settings > Privacy and network > "Keep tracking pixels blocked"; the private preset in onboarding turns it on. Entirely local: signals are read while parsing the body and a bundled domain list ships in-repo. Nothing is fetched or looked up. Signals per remote image: declared width/height of 1 or 0, inline style that hides it, host on the bundled list, an email address in the url (each sufficient alone), plus a long opaque id in the url and being the only image from its host (each weak, two needed). When on, flagged images stay blocked even after remote content is loaded, the blocked-content banner says how many and expands to show each host and the reasons, and every load button has a menu to load them anyway. Detection is heuristic and produces false positives; the ui says "look like".

## VirusTotal integration

Off by default, Settings > External. Requires the user's own API key, stored in the OS keyring. Read-only v3 API: `GET /urls/{base64url_nopad(url)}` and `GET /files/{sha256}`. Never submits or uploads; attachments are looked up by hash only, so an unknown file stays unknown. Auto-scan of links and of attachments are separate toggles, both off by default; on-demand scanning is via right-click on a link or attachment, or the shield button in the message toolbar for a whole message. Verdicts cached locally 7 days, max 25 targets per scan; disabling the integration or clearing the key deletes the cache.

## Sync progress

Status bar shows a progress bar while a sync or a backfill runs, counting message bodies. The total comes from the reconcile plan (what will be fetched), not from the mailbox size, and grows as folders open; before the first folder is reconciled the bar is indeterminate. Settings > Power > "Sync progress bar", on by default. With Settings > Power > "Verbose sync" on, the line also names the mailbox, the account and the imap host and port. The background full check is not a running sync: the Sync button stops after the change pass, and the line reads "Checking folders (n/m)" without the spinner or the bar.

## Sync settings

Settings > Sync & power. "Messages to sync per folder": how many of the newest message bodies are downloaded per folder, default 100, "All" fetches every body newest first. Older mail loads on scroll and opening a message that is not downloaded yet fetches it. "Parallel sync connections": 1 to 5, default 3, how many IMAP sessions one mailbox syncs over; a mailbox can override it in its editor. Sending never waits for these connections. "Full folder check every (days)": default 7, 1/3/7/14/30 or "Manual only" (0); startup and push syncs ask the server only for changes (IMAP CONDSTORE), a folder whose last full check is older than this is re-listed in full in the background at the lowest priority, and manual Sync always queues the full check. Settings > Accounts shows a connection line per mailbox: `IMAP · host:port`.

## Text encoding

Mail that declares no charset, or one nothing knows, is detected from the bytes (ICU port, github.com/gogs/chardet) and converted, so nothing invalid reaches the database or the search index. Applies to bodies and to rfc 2047 encoded-words in headers. Settings > Display > "Text encoding fallback": `auto` (default, detect) or a fixed encoding name. Messages read this way carry a badge in the reading pane. Mail cached before this existed is marked and refetched from the server during sync, a few per folder per sync.

## Importing from another client

Two separate things, two dialogs, both under Settings > Import / Export. "Move in from Thunderbird" reads the profiles Thunderbird keeps on this machine (or one picked by hand) and shows each as a card: name, how many accounts, the addresses, how many local mailboxes and their total size. Picking one lists its accounts and its local mail with checkboxes, so what comes across is a choice. Accounts and mail are separate imports. No password is ever read: Thunderbird keeps its own, and each mailbox asks for one on its first sync. A POP3 account, or one already configured, is listed with the reason rather than hidden. "Import mail files" loads .eml and .mbox from anywhere. Everything imported lands in Local Folders, which sync never touches. Progress reports the mailbox being read and its position in the run, plus bytes; an mbox does not say how many messages it holds until it has been read through.

## Select all

A checkbox above the message list, and Cmd/Ctrl+A while the list has focus. The list only holds the pages that were scrolled to, so how far it reaches is a setting: Settings > List > "Select all selects", one of "the loaded messages, then offer the rest" (default), "everything in the current list", "only the loaded messages". The default selects what is loaded and offers the rest in a banner over the list. Widening asks first above 500 messages and is capped at 50,000, which it says when it hits. It follows whatever the list shows: a folder, a saved view or a search result set. The unified views are left out by default, since one of those lists spans every account; Settings > List > "Select all in unified views" turns both the checkbox and the shortcut on there.

## Failed mailbox sync

A sync run used to report success as long as one account got through, so a mailbox that had stopped syncing was invisible. Each account's last outcome is stored (account_sync_state), so it survives a restart. An account whose last sync failed gets a cloud-off mark next to it in the sidebar, and the status bar says which mailbox failed instead of "Synced". Both open the same dialog: what went wrong (refused password, unreachable server, no password stored, or something else), the server's own words, when the mailbox last synced through, and a button to try that one account again. A mailbox with no password stored keeps its existing warning triangle instead, since that is a different question.

## Message fonts

On by default, Settings > Display > "Let emails use their own fonts". A message's own font families are kept, so designed mail reads the way it was written. No network is involved: the reading pane's CSP limits font-src to `data:`, so a named family resolves from a font already installed or falls back to the reader font. Off, the sanitizer strips `font-family` and `<font face>` and every message renders in Settings > Display > reader font. Font sizes and line heights are kept either way.

## Logs and crash reports

Off by default, Settings > Privacy and network. Nightly builds default them on. Nothing is uploaded: no crash reporter, no endpoint. Logs are files in `<data dir>/logs`, rotating at 2 MB with 3 kept copies (~8 MB cap). Levels: debug, info, warn, error. Secrets (passwords, app passwords, OAuth tokens) are removed by exact-value match wherever they appear, including inside server error strings. Mail content is never logged; subjects and senders only with the separate "Include message subjects and senders" opt-in. Crash reports hold the stack, version, OS and current activity, and are offered on the next launch. `PELTON_DEBUG` env var and `--debug` flag force logging on at debug level over the setting. Settings > About has "Open log folder" and "Copy diagnostics" (version, platform and settings only, no mail or addresses).

## Mail providers

App passwords required by most large providers. iCloud: appleid.apple.com > App-Specific Passwords. Gmail: enable 2-Step Verification, then https://myaccount.google.com/apppasswords.

## Default shortcuts

mod = Cmd on macOS, Ctrl elsewhere. mod+N compose, mod+Enter send (only inside a compose pane), mod+F search, mod+R sync, mod+M add mailbox, mod+, settings, mod+P export PDF, mod+Z undo send/delete/archive, Ctrl+Cmd+F fullscreen. Backspace deletes the selection, or the open message when nothing is selected, and Delete does the same until the binding is changed. Message actions take the webmail letters: R reply, A reply-all, F forward, E archive, S flag, U mark unread, M move to folder, Z snooze. They act on the selection, or on the open message when nothing is selected, and never fire while the user is typing in a field. Mark read, offline download and unsubscribe are unbound; user binds them in Settings > Shortcuts, which also rebinds any of the above. A bound action shows its key beside the matching right-click entry and in the toolbar tooltip, unless Settings > Shortcuts > "Show keyboard shortcut hints in the app" is off.

## Theme format (.peltontheme)

Zip container. `manifest.json` at archive root is the only fixed name; it references everything else.

manifest.json fields:
- manifestVersion (int, required): format version, currently 1; newer than the app understands = import refused
- id (slug, optional): install folder name and update-detection key; defaults to slug of name
- name (required), author, version, description, homepage, license
- base (required): "light" | "dark", the built-in token set that fills unoverridden tokens
- pelton (optional): { "min": "x.y.z", "max": "x.y" } app version range; outside range = warning badge, never a block
- tokens (optional): list of JSON file paths merged in order (later wins), or one inline object
- css (optional): list of CSS files concatenated in order
- preview (optional): screenshot path for the gallery
- icons (optional): map of icon name (tabler name, lowercase kebab, no Icon prefix) to SVG path

Token files: flat maps of token name (no `--` prefix needed) to CSS value. Allowlisted tokens only:
surfaces (surface-base/raised/overlay/sunken/hover, selection-bg, selection-bg-strong), text (text-primary/secondary/tertiary/inverse, link), borders (border-subtle/default/strong, hairline), accent (accent, accent-fg), semantic (success/-bg, warning/-bg, danger/-bg), radii (radius-control/card/none), fonts (font-ui, font-mono), type (fz-meta/label/list/body/heading/title, fw-regular/medium/semibold/bold), elevation (shadow-overlay). Spacing/density tokens are not themeable. Values reject `; { } @ url(` and control chars.

CSS rules: relative url("assets/...") refs are inlined as data: URIs at apply time (bundle fonts/images this way); remote url()/@import are listed at import with a keep-or-strip choice (strip default), the choice is baked into installed files. Caps: 20 MB container, 1 MB CSS total, 5 MB per inlined asset, 256 KB per icon SVG.

Icon SVGs: must use currentColor, are sanitized at import and load (no scripts, no event handlers, no href, no foreignObject, no url()); unknown icon names are ignored for forward compatibility.

Install location: `<data dir>/themes/<id>/`, extracted and hand-editable; Settings > Themes > Reload re-validates and re-reads. Export zips the folder back to a shareable file.

## Legal

- Website: https://pelton.app
- Imprint / Impressum: https://pelton.app/imprint/en/
- Privacy: https://pelton.app/privacy/
