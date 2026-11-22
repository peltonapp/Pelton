-- Every message an import delivered, by destination and fingerprint, so the
-- same message is never delivered to the same place twice. The fingerprint is
-- a hash of the Message-ID and the content, never the content itself. A row
-- for mail kept on this computer goes away with the message, so deleting an
-- imported message and importing again brings it back.
CREATE TABLE import_ledger (
    destination TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    message_id  INTEGER REFERENCES messages(id) ON DELETE CASCADE,
    created_at  TEXT NOT NULL,
    PRIMARY KEY (destination, fingerprint)
);
