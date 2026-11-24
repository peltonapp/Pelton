-- The sender passport: what the user's own receiving server recorded about a
-- message's authentication, and the sender headers kept alongside it. The raw
-- headers are not cached, so this is read once, at sync, from the headers the
-- message arrived with.
--
-- auth_state says how far to believe the rest: 'trusted' when the topmost
-- Authentication-Results header came from the account's own provider,
-- 'untrusted' when the topmost one came from somewhere else (nothing in the
-- row is then a result), 'none' when the message carried no such header.
-- Mail synced before this table has no row at all.
CREATE TABLE IF NOT EXISTS message_trust (
    message_id INTEGER PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
    auth_state TEXT NOT NULL DEFAULT '',
    servid TEXT NOT NULL DEFAULT '',
    spf TEXT NOT NULL DEFAULT '',
    spf_domain TEXT NOT NULL DEFAULT '',
    dkim TEXT NOT NULL DEFAULT '',
    dkim_domain TEXT NOT NULL DEFAULT '',
    -- every domain with a passing signature, space separated
    dkim_domains TEXT NOT NULL DEFAULT '',
    dmarc TEXT NOT NULL DEFAULT '',
    dmarc_policy TEXT NOT NULL DEFAULT '',
    header_from TEXT NOT NULL DEFAULT '',
    -- the sender's own claims, unverified: Return-Path, Sender, the d= of
    -- each DKIM-Signature (space separated), and the first Received-SPF and
    -- ARC-Authentication-Results values, cut short
    return_path TEXT NOT NULL DEFAULT '',
    sender TEXT NOT NULL DEFAULT '',
    signers TEXT NOT NULL DEFAULT '',
    -- the signers whose signature covers List-Unsubscribe and
    -- List-Unsubscribe-Post, which a one-click unsubscribe needs (RFC 8058)
    unsub_signers TEXT NOT NULL DEFAULT '',
    received_spf TEXT NOT NULL DEFAULT '',
    arc TEXT NOT NULL DEFAULT '',
    -- the Disposition-Notification-To address a read receipt was asked for
    read_receipt_to TEXT NOT NULL DEFAULT ''
);

-- What the user said about a sender, by bare address: 'known' ("I know this
-- sender") quiets first-contact notices, 'impostor' ("not who they claim to
-- be") marks their mail from then on. Kept on this device.
CREATE TABLE IF NOT EXISTS sender_marks (
    address TEXT PRIMARY KEY,
    mark TEXT NOT NULL,
    marked_at TEXT NOT NULL
);
