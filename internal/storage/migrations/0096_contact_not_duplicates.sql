-- Pairs of contacts the user said are not duplicates of each other, so the
-- duplicates view stops offering them.
--
-- A pair is stored by the contacts' identities rather than their row ids: a
-- full resync empties a book and writes its contacts back with new ids, and a
-- dismissal has to outlive that. An identity is the vCard UID with the book it
-- is in, or the server path when the card has no UID. The two keys are stored
-- in sorted order, so a pair has one row whichever way round it was named.

CREATE TABLE IF NOT EXISTS contact_not_duplicates (
    key_a TEXT NOT NULL,
    key_b TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (key_a, key_b)
);
