-- pelton:foreign-keys-off

ALTER TABLE folders ADD COLUMN remote_id TEXT NOT NULL DEFAULT '';
ALTER TABLE folders ADD COLUMN state_token TEXT NOT NULL DEFAULT '';
ALTER TABLE folders ADD COLUMN sync_floor_id TEXT NOT NULL DEFAULT '';
ALTER TABLE folders ADD COLUMN sync_initialized INTEGER NOT NULL DEFAULT 0;
UPDATE folders SET remote_id = imap_path WHERE remote_id = '';
CREATE UNIQUE INDEX idx_folders_account_remote ON folders(account_id, remote_id);

-- Rebuild messages so UNIQUE(folder_id, uid) becomes partial. Row ids are copied.
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
    needs_refetch   INTEGER NOT NULL DEFAULT 0
);
INSERT INTO messages_new (
    id, account_id, folder_id, uid, remote_id, message_id, subject, from_address, from_name,
    to_addresses, cc_addresses, date, flags, body_plain, body_html, has_attachments, size_bytes,
    pending_flags, pending_delete, flag_color, snooze_until, snooze_hidden, offline,
    list_unsubscribe, list_unsubscribe_post, smime_status, smime_signer, smime_email,
    smime_issuer, smime_detail, auth_spf, auth_dkim, auth_dmarc, auth_spf_domain,
    auth_dkim_domain, reply_to, smime_certs, smime_fingerprint, charset_guess, needs_refetch
)
SELECT
    id, account_id, folder_id, uid, CAST(uid AS TEXT), message_id, subject, from_address, from_name,
    to_addresses, cc_addresses, date, flags, body_plain, body_html, has_attachments, size_bytes,
    pending_flags, pending_delete, flag_color, snooze_until, snooze_hidden, offline,
    list_unsubscribe, list_unsubscribe_post, smime_status, smime_signer, smime_email,
    smime_issuer, smime_detail, auth_spf, auth_dkim, auth_dmarc, auth_spf_domain,
    auth_dkim_domain, reply_to, smime_certs, smime_fingerprint, charset_guess, needs_refetch
FROM messages;
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
CREATE INDEX idx_messages_needs_refetch ON messages(folder_id) WHERE needs_refetch = 1;
CREATE INDEX idx_messages_smime_fp ON messages(smime_fingerprint) WHERE smime_fingerprint != '';

-- Recreate the external-content FTS triggers exactly as defined by migration
-- 0004, then rebuild the index because the content table was replaced.
CREATE TRIGGER messages_fts_ai AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, subject, body_plain, from_address)
    VALUES (new.id, new.subject, new.body_plain, new.from_address);
END;
CREATE TRIGGER messages_fts_ad AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, subject, body_plain, from_address)
    VALUES ('delete', old.id, old.subject, old.body_plain, old.from_address);
END;
CREATE TRIGGER messages_fts_au AFTER UPDATE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, subject, body_plain, from_address)
    VALUES ('delete', old.id, old.subject, old.body_plain, old.from_address);
    INSERT INTO messages_fts(rowid, subject, body_plain, from_address)
    VALUES (new.id, new.subject, new.body_plain, new.from_address);
END;
INSERT INTO messages_fts(messages_fts) VALUES ('rebuild');
