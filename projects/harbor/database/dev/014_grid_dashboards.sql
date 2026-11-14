-- The seeded dashboards, laid out on the grid every dashboard now has, 12
-- columns by 8 rows: each from its file in assets/dashboards/grid/, by name.
-- As a save does, this forgets which services each dashboard called, until
-- it runs again.
UPDATE dashboards SET source = {{file "assets/dashboards/grid/Family.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Family';
UPDATE dashboards SET source = {{file "assets/dashboards/grid/Home.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Home';
UPDATE dashboards SET source = {{file "assets/dashboards/grid/Issue board.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Issue board';
UPDATE dashboards SET source = {{file "assets/dashboards/grid/Money.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Money';
UPDATE dashboards SET source = {{file "assets/dashboards/grid/This week.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'This week';
UPDATE dashboards SET source = {{file "assets/dashboards/grid/Today at work.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Today at work';

DELETE FROM dashboard_services
WHERE dashboard_id IN (
    SELECT id FROM dashboards WHERE name IN ('Family', 'Home', 'Issue board', 'Money', 'This week', 'Today at work')
);
