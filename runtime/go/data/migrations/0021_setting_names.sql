-- Every setting gets one flat name: the same word in friendo.toml's [settings]
-- block, in the database, in the admin and in the API. Two flip meaning as they
-- go: "what a sign-up becomes" is now a yes/no, and comments are described by
-- whether they wait for review rather than whether they skip it.
UPDATE OR REPLACE site_settings SET key = 'open_signups' WHERE key = 'access.signups_enabled';
UPDATE OR REPLACE site_settings SET key = 'signups_are_contributors',
  value = CASE WHEN value = 'contributor' THEN 'true' ELSE 'false' END WHERE key = 'access.default_role';
UPDATE OR REPLACE site_settings SET key = 'members_can_post' WHERE key = 'content.accept_submissions';
UPDATE OR REPLACE site_settings SET key = 'posts_need_review' WHERE key = 'content.require_approval';
UPDATE OR REPLACE site_settings SET key = 'comments_need_review',
  value = CASE WHEN value = 'true' THEN 'false' ELSE 'true' END WHERE key = 'moderation.auto_approve';
UPDATE OR REPLACE site_settings SET key = 'members_can_start_groups' WHERE key = 'social.member_groups';
UPDATE OR REPLACE site_settings SET key = 'password_login' WHERE key = 'access.password_login';
UPDATE OR REPLACE site_settings SET key = 'profile_visibility' WHERE key = 'social.profile_visibility';
UPDATE OR REPLACE site_settings SET key = 'default_collections' WHERE key = 'content.default_collections';
