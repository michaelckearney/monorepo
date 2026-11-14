-- The seeded dashboards on the kit's one Widget frame, with content inside
-- it, controls in their widgets, and actions: each from its file in
-- assets/dashboards/framework/, by name. As a save does, this forgets which
-- services each dashboard called, until it runs again.
UPDATE dashboards SET source = {{file "assets/dashboards/framework/Family.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Family';
UPDATE dashboards SET source = {{file "assets/dashboards/framework/Home.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Home';
UPDATE dashboards SET source = {{file "assets/dashboards/framework/Issue board.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Issue board';
UPDATE dashboards SET source = {{file "assets/dashboards/framework/Money.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Money';
UPDATE dashboards SET source = {{file "assets/dashboards/framework/This week.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'This week';
UPDATE dashboards SET source = {{file "assets/dashboards/framework/Today at work.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Today at work';

DELETE FROM dashboard_services
WHERE dashboard_id IN (
    SELECT id FROM dashboards WHERE name IN ('Family', 'Home', 'Issue board', 'Money', 'This week', 'Today at work')
);
