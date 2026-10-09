# Pelton e2e

A Playwright suite that drives the real Pelton UI (the Wails dev server in a
browser) against a Stalwart mail server in Docker.

## What it covers

- Onboarding, then Alice and Bob added as IMAP mailboxes.
- Alice and Bob each hold 6,400 seeded messages, including a 1-byte and a
  roughly 5 MB message.
- Scrolling and backfilling a large inbox, opening a synced message, and
  **Messages to sync per folder** set to **All**.
- Search, folder switching, mark unread, archive, select all, forced sync,
  a second compose window, switching accounts.
- Alice sends, Bob replies, Alice sees the reply.

Timings and known gaps from the last run are written to `report.md`.

## Prerequisites

- Docker
- pnpm
- The Wails CLI matching `go.mod` (v2.16.0)
- The test CA trusted by your system: run `scripts/trust-cert-macos.sh` or
  `scripts/trust-cert-linux.sh`. They create the certificates in `certs/` if
  needed and trust `certs/ca.crt`.
- Ports 443, 993, 465 and 18081 (Stalwart's HTTP port, polled by
  `provision.sh`) free on `127.0.0.1`

## Running

```bash
./e2e/run.sh   # or: make e2e
```

The script:

1. Wipes `~/Library/Application Support/Pelton-dev`. The path is
   macOS-only, so on Linux the script does not wipe the dev data directory.
2. Starts Stalwart with `docker-compose.yml` and runs `provision.sh`.
3. Seeds 2 x 6,400 messages for alice and bob with `e2e/seed`.
4. Starts `wails dev` with `PELTON_DEV=1`.
5. Runs Playwright.
6. Stops everything, wipes `Pelton-dev` again and removes the compose
   project, whether the run passed or not.

To reuse a UI that is already running, set `E2E_BASE_URL`:

```bash
E2E_BASE_URL=http://127.0.0.1:34115 ./e2e/run.sh
```

This only runs Playwright. It starts and stops nothing and deletes nothing.
The server and seed data must already be in place.

## Trying it by hand

`e2e/manual.sh` (or `make e2e-manual`) sets up the same Stalwart but skips
Playwright, so you can add the mailboxes yourself and play with mail:

```bash
./e2e/manual.sh   # or: make e2e-manual
```

It wipes `Pelton-dev`, starts Stalwart, runs `provision.sh`, adds carol and
dave, seeds 200 messages each into alice and bob, prints the accounts and
then runs `make run` in the foreground. Same prerequisites as above.

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
  seeding.
- `./e2e/add-user.sh erin` adds `erin@example.org` / `erin-e2e` while
  Stalwart is running.
- Quitting Pelton or pressing Ctrl-C removes the compose project and wipes
  `Pelton-dev`. With `E2E_KEEP=1` both stay; stop Stalwart later with
  `docker compose -p pelton-e2e -f e2e/docker-compose.yml down -v`.
- If a `pelton-e2e` project is already running, the script stops rather
  than take it down. `E2E_FORCE=1` takes it down.

## Order

The Playwright projects run in order. `inbox` goes first: it onboards and
adds both mailboxes. `flows` depends on it and needs both accounts.
