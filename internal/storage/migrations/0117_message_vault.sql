-- vault_id is the vault a message is sealed to, 0 for mail stored in clear.
-- A sealed row keeps only what sync and ordering need; its content is in
-- mail_vault_sealed.
ALTER TABLE messages ADD COLUMN vault_id INTEGER NOT NULL DEFAULT 0;

CREATE INDEX idx_messages_vault ON messages(vault_id) WHERE vault_id != 0;
