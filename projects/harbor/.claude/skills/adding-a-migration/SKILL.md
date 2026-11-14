---
name: adding-a-migration
description: "Change Harbor's database schema or data with a new migration."
---

# Adding a migration

1. Never edit a migration that has run. The dev database holds benchmark
   history worth keeping.
2. Number the new one next across `database/` and `database/<mode>/`
   together: list both, and take one past the highest. A migration every
   mode needs goes in `database/`; one only dev needs, in `database/dev/`.
3. Start it with a comment saying why the change exists and what breaks
   without it, as the others do.
4. Run `harness test -unit` on what reads the change, then the whole `harness test`.
5. A change to a fixture in `database/dev/` reaches only a new dev
   database. Say so in your summary: the owner deletes `.desktop/data/dev`
   to see it.
