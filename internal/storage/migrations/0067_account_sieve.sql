-- How each account reaches its server's sieve service (ManageSieve, RFC 5804)
-- for out of office replies and server side filters.
--
-- Most accounts never get a row: the service is looked for on the imap host,
-- port 4190, with STARTTLS. A row holds what the user set by hand when that
-- guess is wrong, and what the app learned about the server.

CREATE TABLE IF NOT EXISTS account_sieve (
    account_id INTEGER PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    -- host, port and security set by hand. '' and 0 mean the imap host and
    -- port 4190; tls is '' for STARTTLS or 'ssl' for implicit TLS.
    host TEXT NOT NULL DEFAULT '',
    port INTEGER NOT NULL DEFAULT 0,
    tls TEXT NOT NULL DEFAULT '',
    -- 1 once the server was found to have no sieve service, so the app says
    -- so instead of trying again on every visit.
    unsupported INTEGER NOT NULL DEFAULT 0,
    checked_at TEXT NOT NULL DEFAULT '',
    -- the script that was active before Pelton's last save, for "restore
    -- previous". has_previous is 0 until a save has happened.
    previous_active TEXT NOT NULL DEFAULT '',
    has_previous INTEGER NOT NULL DEFAULT 0
);
