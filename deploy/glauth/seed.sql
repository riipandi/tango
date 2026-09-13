-- Seed data for the glauth LDAP dev server (postgres plugin backend).
-- The plugin creates its own schema (users, ldapgroups, includegroups,
-- capabilities) on first start; config [[users]]/[[groups]] entries are
-- NOT imported when datastore = "plugin", so seed via SQL instead.
-- Run idempotently on every container start (post_start in compose.yaml).

INSERT INTO ldapgroups (name, gidnumber)
VALUES ('engineering', 5501), ('operations', 5502), ('tango-admins', 5503)
ON CONFLICT (name) DO NOTHING;

-- All interactive passwords are "dogood"; svcldap binds with "mysecret".
INSERT INTO users (name, uidnumber, primarygroup, mail, givenname, sn, passsha256, othergroups)
VALUES
  ('svcldap',       5001, 5501, 'svcldap@tango.local',       '',      '',       '652c7dc687d98c9889304ed2e408c74b611e86a40caa51c4b43f1dd5913c5cd0', ''),
  ('ada.wong',      5002, 5501, 'ada.wong@tango.local',      'Ada',   'Wong',   '6478579e37aff45f013e14eeb30b3cc56c72ccdc310123bcdf53e0333e3f416a', ''),
  ('budi.santoso',  5003, 5502, 'budi.santoso@tango.local',  'Budi',  'Santoso','6478579e37aff45f013e14eeb30b3cc56c72ccdc310123bcdf53e0333e3f416a', ''),
  ('carla.mendez',  5004, 5502, 'carla.mendez@tango.local',  'Carla', 'Mendez', '6478579e37aff45f013e14eeb30b3cc56c72ccdc310123bcdf53e0333e3f416a', '5503')
ON CONFLICT (name) DO NOTHING;

-- svcldap is the sync service account: search across the whole tree.
INSERT INTO capabilities (userid, action, object) VALUES (5001, 'search', '*') ON CONFLICT DO NOTHING;
