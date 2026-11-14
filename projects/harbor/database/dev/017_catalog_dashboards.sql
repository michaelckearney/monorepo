-- The seeded dashboards on the catalog's widgets, each a typed component
-- that shows what the dashboard gives it, with the services' tools reached
-- through useService: each from its file in assets/dashboards/catalog/, by
-- name. Which services each uses, its file says; showing it notes them.
UPDATE dashboards SET source = {{file "assets/dashboards/catalog/Family.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Family';
UPDATE dashboards SET source = {{file "assets/dashboards/catalog/Home.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Home';
UPDATE dashboards SET source = {{file "assets/dashboards/catalog/Issue board.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Issue board';
UPDATE dashboards SET source = {{file "assets/dashboards/catalog/Money.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Money';
UPDATE dashboards SET source = {{file "assets/dashboards/catalog/This week.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'This week';
UPDATE dashboards SET source = {{file "assets/dashboards/catalog/Today at work.vue"}}, updated_at = CURRENT_TIMESTAMP WHERE name = 'Today at work';

DELETE FROM dashboard_services
WHERE dashboard_id IN (
    SELECT id FROM dashboards WHERE name IN ('Family', 'Home', 'Issue board', 'Money', 'This week', 'Today at work')
);
INSERT INTO dashboard_services (dashboard_id, service)
SELECT d.id, s.service FROM dashboards d JOIN (
    SELECT 'Family' AS name, 'maplegrove' AS service UNION ALL SELECT 'Family', 'clearwater' UNION ALL SELECT 'Family', 'shoebox'
    UNION ALL SELECT 'Home', 'homebase' UNION ALL SELECT 'Home', 'homebase-router' UNION ALL SELECT 'Home', 'brightline'
    UNION ALL SELECT 'Issue board', 'tasklane'
    UNION ALL SELECT 'Money', 'tidewater' UNION ALL SELECT 'Money', 'brightline' UNION ALL SELECT 'Money', 'payday'
    UNION ALL SELECT 'This week', 'clearwater' UNION ALL SELECT 'This week', 'maplegrove' UNION ALL SELECT 'This week', 'brightline'
    UNION ALL SELECT 'This week', 'payday' UNION ALL SELECT 'This week', 'parcel'
    UNION ALL SELECT 'Today at work', 'tasklane' UNION ALL SELECT 'Today at work', 'huddle' UNION ALL SELECT 'Today at work', 'payday'
) s ON s.name = d.name;
