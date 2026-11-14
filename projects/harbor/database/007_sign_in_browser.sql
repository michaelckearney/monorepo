-- A live view shows the service's tab, by its ID in Slipway's browser, and
-- the tabs it opens; prompt is what Harbor asks you beside the page, as
-- JSON, or NULL. Harbor's browser no longer has a window of its own, so the
-- setting for one goes.
ALTER TABLE live_views ADD COLUMN tab TEXT NOT NULL DEFAULT '';
ALTER TABLE live_views ADD COLUMN prompt TEXT;
DELETE FROM settings WHERE key = 'browser_visible';
