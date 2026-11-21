-- A colour per mailbox (#486), marking its mail in views that mix accounts.
-- '' means none; otherwise one of the palette names the frontend offers.
ALTER TABLE accounts ADD COLUMN color TEXT NOT NULL DEFAULT '';
