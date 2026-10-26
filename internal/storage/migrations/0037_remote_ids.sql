-- pelton:foreign-keys-off

ALTER TABLE folders ADD COLUMN remote_id TEXT NOT NULL DEFAULT '';
ALTER TABLE folders ADD COLUMN state_token TEXT NOT NULL DEFAULT '';
ALTER TABLE folders ADD COLUMN sync_floor_id TEXT NOT NULL DEFAULT '';
ALTER TABLE folders ADD COLUMN sync_initialized INTEGER NOT NULL DEFAULT 0;
UPDATE folders SET remote_id = imap_path WHERE remote_id = '';
-- The window floor moves from a uid to a remote id. An IMAP remote id is the
-- decimal uid, as for messages below; without this every floored folder would
-- lose its window and download in full.
UPDATE folders SET sync_floor_id = CAST(sync_floor_uid AS TEXT) WHERE sync_floor_uid > 0;
CREATE UNIQUE INDEX idx_folders_account_remote ON folders(account_id, remote_id);

-- Rebuild messages so UNIQUE(folder_id, uid) becomes partial. Row ids are copied.
--
-- search_state is what the search index holds for the row: 0 nothing, 1 the
-- headers (and preview) of a stub, 2 the full body. The index pass re-indexes
-- complete rows below 2, so a body that lands after its stub was indexed (or
-- a crash between storing a body and indexing it) heals on the next pass.
-- Copied rows are full mail the id watermark already covers, so they start
-- at 2; the few above the watermark are picked up by it anyway.
--
-- text_rev counts rewrites of the indexed text (body fill, charset repair,
-- a list refresh that changes it).
-- An index pass records its state only if text_rev is still what it read, so
-- a rewrite during the pass is indexed again whatever state the row had.
--
-- body_complete is 0 for a list stub and 1 once the body is fetched; preview
-- text can sit in body_plain on a stub, so empty body columns are no signal.
-- Every row here came from a full fetch, so it is copied as complete. Adding
-- it in the rebuild spares a second full-table UPDATE (once 0038).
DROP TRIGGER messages_fts_ai;
DROP TRIGGER messages_fts_ad;
DROP TRIGGER messages_fts_au;
CREATE TABLE messages_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id      INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    folder_id       INTEGER NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
    uid             INTEGER NOT NULL,
    remote_id       TEXT    NOT NULL DEFAULT '',
    message_id      TEXT    NOT NULL DEFAULT '',
    subject         TEXT    NOT NULL DEFAULT '',
    from_address    TEXT    NOT NULL DEFAULT '',
    from_name       TEXT    NOT NULL DEFAULT '',
    to_addresses    TEXT    NOT NULL DEFAULT '',
    cc_addresses    TEXT    NOT NULL DEFAULT '',
    date            TEXT    NOT NULL DEFAULT '',
    flags           INTEGER NOT NULL DEFAULT 0,
    body_plain      TEXT    NOT NULL DEFAULT '',
    body_html       TEXT    NOT NULL DEFAULT '',
    has_attachments INTEGER NOT NULL DEFAULT 0,
    size_bytes      INTEGER NOT NULL DEFAULT 0,
    pending_flags   INTEGER NOT NULL DEFAULT 0,
    pending_delete  INTEGER NOT NULL DEFAULT 0,
    flag_color      INTEGER NOT NULL DEFAULT 0,
    snooze_until    TEXT    NOT NULL DEFAULT '',
    snooze_hidden   INTEGER NOT NULL DEFAULT 0,
    offline         INTEGER NOT NULL DEFAULT 0,
    list_unsubscribe TEXT   NOT NULL DEFAULT '',
    list_unsubscribe_post INTEGER NOT NULL DEFAULT 0,
    smime_status    TEXT    NOT NULL DEFAULT '',
    smime_signer    TEXT    NOT NULL DEFAULT '',
    smime_email     TEXT    NOT NULL DEFAULT '',
    smime_issuer    TEXT    NOT NULL DEFAULT '',
    smime_detail    TEXT    NOT NULL DEFAULT '',
    auth_spf        TEXT    NOT NULL DEFAULT '',
    auth_dkim       TEXT    NOT NULL DEFAULT '',
    auth_dmarc      TEXT    NOT NULL DEFAULT '',
    auth_spf_domain TEXT    NOT NULL DEFAULT '',
    auth_dkim_domain TEXT   NOT NULL DEFAULT '',
    reply_to        TEXT    NOT NULL DEFAULT '',
    smime_certs     BLOB    NOT NULL DEFAULT x'',
    smime_fingerprint TEXT  NOT NULL DEFAULT '',
    charset_guess   TEXT    NOT NULL DEFAULT '',
    needs_refetch   INTEGER NOT NULL DEFAULT 0,
    body_complete   INTEGER NOT NULL DEFAULT 0,
    search_state    INTEGER NOT NULL DEFAULT 0,
    text_rev        INTEGER NOT NULL DEFAULT 0
);
INSERT INTO messages_new (
    id, account_id, folder_id, uid, remote_id, message_id, subject, from_address, from_name,
    to_addresses, cc_addresses, date, flags, body_plain, body_html, has_attachments, size_bytes,
    pending_flags, pending_delete, flag_color, snooze_until, snooze_hidden, offline,
    list_unsubscribe, list_unsubscribe_post, smime_status, smime_signer, smime_email,
    smime_issuer, smime_detail, auth_spf, auth_dkim, auth_dmarc, auth_spf_domain,
    auth_dkim_domain, reply_to, smime_certs, smime_fingerprint, charset_guess, needs_refetch,
    body_complete, search_state
)
SELECT
    id, account_id, folder_id, uid, CAST(uid AS TEXT), message_id, subject, from_address, from_name,
    to_addresses, cc_addresses, date, flags, body_plain, body_html, has_attachments, size_bytes,
    pending_flags, pending_delete, flag_color, snooze_until, snooze_hidden, offline,
    list_unsubscribe, list_unsubscribe_post, smime_status, smime_signer, smime_email,
    smime_issuer, smime_detail, auth_spf, auth_dkim, auth_dmarc, auth_spf_domain,
    auth_dkim_domain, reply_to, smime_certs, smime_fingerprint, charset_guess, needs_refetch,
    1, 2
FROM messages;
-- Carry the AUTOINCREMENT high-water mark over. Without it ids of deleted
-- messages would be handed out again, and the search watermark, stale FTS
-- entries and attachments/<account>/<id>/ dirs would all point at new mail.
DELETE FROM sqlite_sequence WHERE name = 'messages_new';
INSERT INTO sqlite_sequence (name, seq) SELECT 'messages_new', seq FROM sqlite_sequence WHERE name = 'messages';
DROP TABLE messages;
ALTER TABLE messages_new RENAME TO messages;
CREATE UNIQUE INDEX idx_messages_remote ON messages(folder_id, remote_id);
CREATE UNIQUE INDEX idx_messages_uid ON messages(folder_id, uid) WHERE uid != 0;
CREATE INDEX idx_messages_account ON messages(account_id);
CREATE INDEX idx_messages_message_id ON messages(message_id);
CREATE INDEX idx_messages_pending ON messages(folder_id) WHERE pending_flags = 1 OR pending_delete = 1;
CREATE INDEX idx_messages_snooze ON messages(snooze_until) WHERE snooze_until != '';
CREATE INDEX idx_messages_list ON messages(folder_id, date DESC, uid DESC);
CREATE INDEX idx_messages_state ON messages(folder_id, flags, pending_delete, snooze_hidden);
CREATE INDEX idx_messages_search_pending ON messages(id) WHERE body_complete = 1 AND search_state < 2;
CREATE INDEX idx_messages_needs_refetch ON messages(folder_id) WHERE needs_refetch = 1;
-- Dropping the old table dropped 0027's idx_messages_smime_fingerprint; it is
-- recreated under the shorter name idx_messages_smime_fp (same column, same
-- WHERE). Nothing refers to index names, so only the name changes.
CREATE INDEX idx_messages_smime_fp ON messages(smime_fingerprint) WHERE smime_fingerprint != '';

-- Recreate the external-content FTS triggers from migration 0004. Row ids and
-- the indexed text were copied unchanged, so the index still matches and needs
-- no rebuild. The update trigger now fires only when an indexed column or the
-- rowid changes, so flag and state updates no longer rewrite the index.
CREATE TRIGGER messages_fts_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, subject, body_plain, from_address)
    VALUES (new.id, new.subject, new.body_plain, new.from_address);
END;
CREATE TRIGGER messages_fts_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, subject, body_plain, from_address)
    VALUES ('delete', old.id, old.subject, old.body_plain, old.from_address);
END;
CREATE TRIGGER messages_fts_au AFTER UPDATE OF id, subject, body_plain, from_address ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, subject, body_plain, from_address)
    VALUES ('delete', old.id, old.subject, old.body_plain, old.from_address);
    INSERT INTO messages_fts(rowid, subject, body_plain, from_address)
    VALUES (new.id, new.subject, new.body_plain, new.from_address);
END;
