-- The seeded dashboards, moved onto the kit's widgets: each from its file in
-- assets/dashboards/kit/, by name. 010_fixtures.sql's files are left as
-- they were, and use pieces the kit no longer has. As a save does, this
-- forgets which services each dashboard called, until it runs again.
UPDATE dashboards SET source = {{file "assets/dashboards/kit/Family.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Family';
UPDATE dashboards SET source = {{file "assets/dashboards/kit/Home.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Home';
UPDATE dashboards SET source = {{file "assets/dashboards/kit/Issue board.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Issue board';
UPDATE dashboards SET source = {{file "assets/dashboards/kit/Money.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Money';
UPDATE dashboards SET source = {{file "assets/dashboards/kit/This week.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'This week';
UPDATE dashboards SET source = {{file "assets/dashboards/kit/Today at work.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Today at work';

DELETE FROM dashboard_services
WHERE dashboard_id IN (
    SELECT id FROM dashboards WHERE name IN ('Family', 'Home', 'Issue board', 'Money', 'This week', 'Today at work')
);
