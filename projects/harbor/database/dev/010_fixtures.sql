-- The fixtures a developer build starts with: a service for each site in
-- harbor-fixtures, and dashboards that use them. Each is its own file in
-- assets/, as it would be written in Harbor's editor. Harbor reads a
-- service's manifest from its source when it starts (manifests.go), so only
-- the name and source are here.

INSERT INTO services (name, source) VALUES ('brightline', {{file "assets/services/brightline.ts"}});
INSERT INTO services (name, source) VALUES ('clearwater', {{file "assets/services/clearwater.ts"}});
INSERT INTO services (name, source) VALUES ('homebase-router', {{file "assets/services/homebase-router.ts"}});
INSERT INTO services (name, source) VALUES ('homebase', {{file "assets/services/homebase.ts"}});
INSERT INTO services (name, source) VALUES ('huddle', {{file "assets/services/huddle.ts"}});
INSERT INTO services (name, source) VALUES ('maplegrove', {{file "assets/services/maplegrove.ts"}});
INSERT INTO services (name, source) VALUES ('parcel', {{file "assets/services/parcel.ts"}});
INSERT INTO services (name, source) VALUES ('payday', {{file "assets/services/payday.ts"}});
INSERT INTO services (name, source) VALUES ('shoebox', {{file "assets/services/shoebox.ts"}});
INSERT INTO services (name, source) VALUES ('tasklane', {{file "assets/services/tasklane.ts"}});
INSERT INTO services (name, source) VALUES ('tidewater', {{file "assets/services/tidewater.ts"}});

INSERT INTO dashboards (name, source) VALUES ('Family', {{file "assets/dashboards/Family.vue"}});
INSERT INTO dashboards (name, source) VALUES ('Home', {{file "assets/dashboards/Home.vue"}});
INSERT INTO dashboards (name, source) VALUES ('Issue board', {{file "assets/dashboards/Issue board.vue"}});
INSERT INTO dashboards (name, source) VALUES ('Money', {{file "assets/dashboards/Money.vue"}});
INSERT INTO dashboards (name, source) VALUES ('This week', {{file "assets/dashboards/This week.vue"}});
INSERT INTO dashboards (name, source) VALUES ('Today at work', {{file "assets/dashboards/Today at work.vue"}});
