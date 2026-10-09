-- body_complete distinguishes a list stub from a message whose body has been
-- fetched. Preview text can sit in body_plain on a stub, so emptiness of the
-- body columns is not a reliable signal.
--
-- Rows already in the database were written by a full fetch, so they start
-- complete. New stubs stay 0 until a body fill.

ALTER TABLE messages ADD COLUMN body_complete INTEGER NOT NULL DEFAULT 0;
UPDATE messages SET body_complete = 1;
