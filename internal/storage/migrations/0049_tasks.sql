-- Tasks: VTODOs kept in calendar collections next to the events.
--
-- A calendar says which components it holds. A CalDAV collection reports
-- that in supported-calendar-component-set; a local calendar is made for
-- events or for tasks. Rows that were here before hold events.

ALTER TABLE calendars ADD COLUMN components TEXT NOT NULL DEFAULT 'VEVENT';

-- the component a calendar object resource carries: 'VEVENT' or 'VTODO'. A
-- task syncs, queues and conflicts exactly like an event; only what is shown
-- of it differs.
ALTER TABLE calendar_events ADD COLUMN kind TEXT NOT NULL DEFAULT 'VEVENT';

-- for a task: when it is due, as a date (2006-01-02) or a utc instant, empty
-- for none; when it was completed, empty while open; and the Message-IDs of
-- the mails it links to, one per line, for marking those mails in the list.
ALTER TABLE calendar_events ADD COLUMN task_due TEXT NOT NULL DEFAULT '';
ALTER TABLE calendar_events ADD COLUMN task_completed TEXT NOT NULL DEFAULT '';
ALTER TABLE calendar_events ADD COLUMN task_mail TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_calendar_events_kind ON calendar_events(kind, calendar_id);

-- one local task list, so a task has somewhere to go before any server is
-- set up. The empty name is shown as the ui's own label for it.
INSERT INTO calendars (calendar_account_id, name, components, position, created_at)
VALUES (NULL, '', 'VTODO', 0, strftime('%Y-%m-%dT%H:%M:%SZ', 'now'));
