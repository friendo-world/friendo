-- Reminders: an RSVP may carry an email to remind the day before the date it
-- answers for. A visitor (no account, no email) gives one when they want it;
-- it's for the reminder only — organizers never see it. reminder_link is the
-- event page's address as the browser saw it, since the server has no request
-- in hand when the reminder goes out. reminder_sent is set once it has.
ALTER TABLE rsvps ADD COLUMN reminder_email TEXT NOT NULL DEFAULT '';
ALTER TABLE rsvps ADD COLUMN reminder_link  TEXT NOT NULL DEFAULT '';
ALTER TABLE rsvps ADD COLUMN reminder_sent  TEXT NOT NULL DEFAULT '';
