-- A block keeps a title only when the user typed it. A title that comes from
-- the block's mail or task is read from them when the block is shown, so a
-- mail that later moves into a vault leaves no copy of its subject here.
ALTER TABLE time_blocks ADD COLUMN title_own INTEGER NOT NULL DEFAULT 0;

-- a title on a block with neither mail nor task was typed by the user.
UPDATE time_blocks SET title_own = 1 WHERE title != '' AND message_id = 0 AND message_header = '' AND task_uid = '';

-- a block for a mail or a task got its title copied from it unless the user
-- renamed it, and the two cannot be told apart here: the copies go, and such
-- a block shows its mail's or task's title again.
UPDATE time_blocks SET title = '' WHERE title_own = 0;
