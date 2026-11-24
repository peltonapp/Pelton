-- mail_vaults holds what each mail vault stores about its keys. The data key
-- is kept only wrapped: once under a key derived from the passphrase and once
-- under a key derived from the recovery code, each with its own salt and
-- argon2id parameters. public_key seals mail while the vault is locked;
-- private_box is its private half sealed under the data key. link_key keys
-- the hashes sealed messages are threaded by. state is sealing, ready or
-- unsealing, so an interrupted job resumes where it stopped.
CREATE TABLE mail_vaults (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    uid               BLOB    NOT NULL,
    name              TEXT    NOT NULL DEFAULT '',
    created_at        TEXT    NOT NULL,
    pass_salt         BLOB    NOT NULL,
    pass_time         INTEGER NOT NULL,
    pass_memory       INTEGER NOT NULL,
    pass_threads      INTEGER NOT NULL,
    pass_box          BLOB    NOT NULL,
    rec_salt          BLOB    NOT NULL,
    rec_time          INTEGER NOT NULL,
    rec_memory        INTEGER NOT NULL,
    rec_threads       INTEGER NOT NULL,
    rec_box           BLOB    NOT NULL,
    public_key        BLOB    NOT NULL,
    private_box       BLOB    NOT NULL,
    link_key          BLOB    NOT NULL,
    auto_lock_minutes INTEGER NOT NULL DEFAULT 15,
    notify            TEXT    NOT NULL DEFAULT 'generic',
    hide_locked       INTEGER NOT NULL DEFAULT 0,
    allow_scripts     INTEGER NOT NULL DEFAULT 0,
    allow_mcp         INTEGER NOT NULL DEFAULT 0,
    failures          INTEGER NOT NULL DEFAULT 0,
    retry_at          TEXT    NOT NULL DEFAULT '',
    state             TEXT    NOT NULL DEFAULT 'sealing',
    job_total         INTEGER NOT NULL DEFAULT 0
);

-- mail_vault_members is what the user put in a vault: a folder, or a whole
-- account when folder_id is 0.
CREATE TABLE mail_vault_members (
    vault_id   INTEGER NOT NULL REFERENCES mail_vaults(id) ON DELETE CASCADE,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    folder_id  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (account_id, folder_id)
);

-- mail_vault_folders is every folder a vault covers, the members resolved.
CREATE TABLE mail_vault_folders (
    folder_id INTEGER PRIMARY KEY REFERENCES folders(id) ON DELETE CASCADE,
    vault_id  INTEGER NOT NULL REFERENCES mail_vaults(id) ON DELETE CASCADE
);

CREATE INDEX idx_mail_vault_folders_vault ON mail_vault_folders(vault_id);

-- a folder that appears later in a vaulted account belongs to the vault from
-- its first row on, so nothing synced into it is ever stored in clear.
CREATE TRIGGER mail_vault_new_folder AFTER INSERT ON folders
WHEN EXISTS (SELECT 1 FROM mail_vault_members WHERE account_id = new.account_id AND folder_id = 0)
BEGIN
    INSERT OR IGNORE INTO mail_vault_folders (folder_id, vault_id)
    SELECT new.id, vault_id FROM mail_vault_members
    WHERE account_id = new.account_id AND folder_id = 0;
END;

-- mail_vault_sealed holds the sealed content of a message: the ephemeral
-- public key its key is derived from, then the envelope, the bodies and, for
-- pgp mail, the raw source, each sealed to its row and column.
CREATE TABLE mail_vault_sealed (
    message_id INTEGER PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
    vault_id   INTEGER NOT NULL,
    epk        BLOB    NOT NULL,
    envelope   BLOB    NOT NULL,
    body       BLOB    NOT NULL,
    source     BLOB
);

CREATE INDEX idx_mail_vault_sealed_vault ON mail_vault_sealed(vault_id);

-- mail_vault_wipes lists attachment files left in clear by a sealing step that
-- committed, until they have been overwritten and removed.
CREATE TABLE mail_vault_wipes (
    path TEXT PRIMARY KEY
);
