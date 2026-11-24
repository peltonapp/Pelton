-- mail_kind sorts a message for the split inbox. Bit 1 marks mailing-list mail
-- (it carried List-Unsubscribe or List-Id), bit 2 an automated sender
-- (noreply@, notifications@, mailer-daemon@ and the like). It is worked out
-- once on insert so the split queries read a column instead of parsing
-- addresses for every row they count.
ALTER TABLE messages ADD COLUMN mail_kind INTEGER NOT NULL DEFAULT 0;

-- mail cached before this column existed: List-Id was never stored, so only
-- List-Unsubscribe can mark it as list mail.
UPDATE messages SET mail_kind = 1 WHERE list_unsubscribe != '';

-- from_address holds either a bare address or "Name <address>"; the local part
-- is what the sender test reads. The patterns match automatedLocalParts in
-- splits.go.
UPDATE messages SET mail_kind = mail_kind | 2
FROM (
    SELECT id AS mid, LOWER(TRIM(CASE WHEN INSTR(from_address, '<') > 0
                                      THEN SUBSTR(from_address, INSTR(from_address, '<') + 1)
                                      ELSE from_address END, ' >')) AS a
    FROM messages
) AS s
WHERE s.mid = messages.id AND (
    s.a LIKE 'noreply%' OR s.a LIKE 'no-reply%' OR s.a LIKE 'no\_reply%' ESCAPE '\'
    OR s.a LIKE 'donotreply%' OR s.a LIKE 'do-not-reply%' OR s.a LIKE 'do\_not\_reply%' ESCAPE '\'
    OR s.a LIKE 'notification%' OR s.a LIKE 'notify%' OR s.a LIKE 'mailer-daemon%'
    OR s.a LIKE 'postmaster@%'
);

-- the split counts read these columns for every inbox row, so the index holds
-- all of them and the grouped count never touches the row store.
CREATE INDEX idx_messages_split ON messages(folder_id, pending_delete, snooze_hidden, mail_kind, from_address, flags, thread_id);
