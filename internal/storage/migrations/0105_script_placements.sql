-- Where a script is offered in the app (context menus, toolbars, the
-- command palette), as a JSON list, and what a run's log adds to its steps:
-- the line or block it failed in and the blocks it never got to, as JSON.

ALTER TABLE scripts ADD COLUMN placements TEXT NOT NULL DEFAULT '[]';
ALTER TABLE script_runs ADD COLUMN detail TEXT NOT NULL DEFAULT '{}';
