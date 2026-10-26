# Pelton e2e

A Playwright suite that drives the real Pelton UI (the Wails dev server in a
browser) against a Stalwart mail server in Docker.

## What it covers

- Onboarding, then Alice and Bob added as IMAP mailboxes.
- Alice and Bob each hold 6,400 seeded messages, including a 1-byte and a
  roughly 5 MB message. Those two and a search target are appended last and
  dated newest, so they land in the first 100-message sync window and at the
  top of the list on every run.
- Scrolling a large inbox until backfill fetches older mail, opening a
  synced message, and **Messages to sync per folder** set to **All**.
- Search, folder switching, mark unread, archive, select all, forced sync,
  two compose windows open at once, switching accounts.
- Alice sends, Bob replies, Alice sees the reply.

Timings from the last run are written to `report.md`.

## Platforms

The suite has been run on macOS only. The scripts also handle Linux: the
Linux data directory, `$(WEBKIT_TAGS)` for webkit2gtk-4.1, the system trust
store, and Ctrl instead of Cmd for shortcuts. It has not been run there yet,
so expect to fix things on a first Linux run. On Linux the app also needs a
running Secret Service (GNOME Keyring or KWallet) for account passwords.

## Prerequisites

- Docker, running
- Go, make, curl and pnpm
- The Wails CLI matching `go.mod` (v2.16.0)
- The `sqlite3` CLI: `flows.spec.ts` reads the app's database with it.
  macOS ships it; on Debian/Ubuntu install `sqlite3`.
- The test CA trusted: run `scripts/trust-cert-macos.sh` (your user only:
  login keychain, user trust settings) or `scripts/trust-cert-linux.sh`
  (system trust store, needs sudo). They create the certificates in `certs/`
  if needed and trust `certs/ca.crt`. See [The test CA](#the-test-ca).
- Ports 443, 993, 465 and 18081 (Stalwart's HTTP port, polled by
  `provision.sh`) free on `127.0.0.1`. 443 is often taken by a local proxy
  or rootless Docker setup; move it with `E2E_HTTPS_PORT=8443`, and 18081
  with `E2E_HTTP_PORT`. Nothing in the suite connects to 443. IMAP 993 and
  SMTP 465 cannot move: the tests check that the account form fills them in.
- Port 34125 free on `127.0.0.1` for the e2e app's Wails dev server; move it
  with `E2E_DEVSERVER_PORT`. It is never 34115, the dev server `make run`
  uses: `wails dev` only logs a port it cannot bind, so a shared port would
  hand Playwright the `Pelton-dev` app.

## The test CA

Pelton checks Stalwart's certificate like any other server's, so the suite
needs a CA it trusts. On macOS `trust-cert-macos.sh` trusts it for your user
only (login keychain, user trust settings), not machine-wide in the admin
domain. On Linux Go and WebKitGTK read the system store, so
`trust-cert-linux.sh` installs it there with sudo. `scripts/create-certs.sh` (called by the trust
scripts) makes one with these limits:

- Name constraints: it can only vouch for `127.0.0.1` and `localhost`, and
  `pathlen:0` stops it from signing another CA. A browser or mail client
  rejects anything it signs for another host.
- Its private key lives in a temporary directory just long enough to sign the
  server certificate, then it is deleted. `certs/` holds `ca.crt`, the server
  certificate and the server key, nothing that can sign.

Remove it when you are done, or before making new certificates:

```bash
./e2e/scripts/untrust-cert-macos.sh   # login keychain, user and admin trust settings
./e2e/scripts/untrust-cert-linux.sh   # system trust store, needs sudo
```

They remove every certificate named `Pelton e2e Local CA`, including ones
from older versions of these scripts, and delete `certs/`. On macOS the
untrust script then checks `security dump-trust-settings` (user) and
`security dump-trust-settings -d` (admin) and fails, keeping the certificates
and `certs/`, while either still lists the CA. `trust-cert-macos.sh` refuses
to run while an older version's machine-wide (admin) trust is still there.

`certs/` is mode 0755 and `certs/server.key` 0600; both are bind-mounted
read-only into the Stalwart container. Docker Desktop on macOS reads the key
regardless. On Linux the container reads it with your file permissions; if
Stalwart cannot load the certificate because its user cannot read the key, run
`chmod 0644 e2e/certs/server.key`. The key is only good for `127.0.0.1` and
`localhost` under this CA.

`create-certs.sh` never replaces a CA silently. It stops and points at the
untrust script when:

- `certs/ca.key` exists. Older versions kept the key there and made a CA
  without name constraints.
- `certs/ca.crt` has no name constraints.
- `certs/ca.crt` is there but the server certificate is missing or does not
  verify.
- `certs/` is gone but a `Pelton e2e Local CA` is still trusted.

## Running

```bash
./e2e/run.sh   # or: make e2e
```

The script:

1. Checks the prerequisites before it starts or deletes anything: the tools
   on `PATH`, Docker running, `certs/` present, no `pelton-e2e` project
   running and the ports free. A running project may be another session's,
   so it stops there; `E2E_FORCE=1` takes it down instead.
2. Starts Stalwart with `docker-compose.yml` and runs `provision.sh`.
3. Seeds 2 x 6,400 messages for alice and bob with `e2e/seed`.
4. Starts the app with `make e2e-app`: `wails dev` built as channel `e2e`,
   its dev server on `127.0.0.1:34125` (`E2E_DEVSERVER_PORT`).
5. Runs Playwright. Before any test acts, each spec checks through the app's
   bound methods that it is talking to an `e2e` channel build whose data
   directory is `Pelton-e2e*` (and the directory of `E2E_DB`, if set), and
   stops otherwise.
6. On exit, passed or not: stops the processes it started (and no others),
   deletes the app's data and keyring entries and removes the compose
   project it created.

### Where the app keeps its data

The app picks its data directory and keyring service from its build channel.
`make e2e-app` builds it as channel `e2e` and drops `PELTON_DEV`, which
would otherwise win, so the suite runs against:

| | Data directory | Keyring service |
| --- | --- | --- |
| macOS | `~/Library/Application Support/Pelton-e2e` | `Pelton-e2e` |
| Linux | `${XDG_CONFIG_HOME:-~/.config}/Pelton-e2e` | `Pelton-e2e` |

`Pelton-dev`, which `make run` uses in every worktree, is never touched.
The script deletes `Pelton-e2e` before and after the run, and removes the
`Pelton-e2e` keyring entries with `security delete-generic-password` on macOS
or `secret-tool clear` on Linux (skipped if `secret-tool` is not installed).
`manual.sh` does the same with `Pelton-e2e-manual`.

### Reusing a running UI

To reuse an e2e app that is already running (`make e2e-app`), set
`E2E_BASE_URL` to its dev server:

```bash
E2E_BASE_URL=http://127.0.0.1:34125 ./e2e/run.sh
```

This only runs Playwright. It starts and stops nothing and deletes nothing.
The server and seed data must already be in place. The URL must be an `e2e`
channel build: the suite refuses anything else, including the `make run`
app on 34115. `flows.spec.ts` reads the app's database through `E2E_DB`,
which defaults to the `Pelton-e2e` path above; set it if the running UI uses
another data directory.

## Trying it by hand

`e2e/manual.sh` (or `make e2e-manual`) sets up the same Stalwart but skips
Playwright, so you can add the mailboxes yourself and play with mail:

```bash
./e2e/manual.sh   # or: make e2e-manual
```

It runs the same checks as `run.sh`, starts Stalwart, runs `provision.sh`,
adds carol and dave, seeds 200 messages each into alice and bob, prints the
accounts and then runs `make e2e-app E2E_CHANNEL=e2e-manual` in the
foreground. The app keeps its data in `Pelton-e2e-manual`, next to
`Pelton-e2e`. Same prerequisites as above.

| Account             | Password    | Inbox        |
| ------------------- | ----------- | ------------ |
| `alice@example.org` | `alice-e2e` | seeded       |
| `bob@example.org`   | `bob-e2e`   | seeded       |
| `carol@example.org` | `carol-e2e` | empty        |
| `dave@example.org`  | `dave-e2e`  | empty        |

In Pelton pick **Other (IMAP / SMTP)**, use `127.0.0.1` for both hosts
and keep the default ports (IMAP 993, SMTP 465). Mail between `@example.org` accounts is
delivered locally, so you can send from one mailbox and read it in another.

- `E2E_SEED_PER_INBOX=6400 ./e2e/manual.sh` seeds the full corpus, `0` skips
  seeding. The seed index goes to `manual-manifest.json`, so it does not
  replace the `manifest.json` that `run.sh` checks against.
- `./e2e/add-user.sh erin` adds `erin@example.org` / `erin-e2e` while
  Stalwart is running.
- Quitting Pelton or pressing Ctrl-C removes the compose project and deletes
  `Pelton-e2e-manual` and its keyring entries. With `E2E_KEEP=1` they stay; stop Stalwart later with
  `docker compose -p pelton-e2e -f e2e/docker-compose.yml down -v`.
- If a `pelton-e2e` project is already running, the script stops rather
  than take it down. `E2E_FORCE=1` takes it down.

## Order

The Playwright projects run in order. `inbox` goes first: it onboards and
adds both mailboxes. `flows` depends on it and needs both accounts.
