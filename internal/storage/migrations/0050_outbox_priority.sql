-- priority orders the queue ahead of insertion order: the worker claims the
-- due row with the highest priority first, then the oldest. Moving a message
-- to the front gives it one more than the highest priority in the outbox, so
-- the last one moved goes next. 0 is the plain first in, first out order.
ALTER TABLE outbox ADD COLUMN priority INTEGER NOT NULL DEFAULT 0;
