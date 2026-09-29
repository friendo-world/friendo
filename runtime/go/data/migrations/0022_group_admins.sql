-- Groups mirror the site: admins run a group, moderators keep it tidy. Until
-- now a group's "moderators" did both, so they become its admins.
UPDATE memberships SET role = 'admin' WHERE role = 'moderator';
