---
name: anime-catalog-migration
description: Continue this repository's Bangumi Archive catalog and typed image migration using docs/progress/MASTER.md. Applies only to this migration, not ordinary anime maintenance.
---

Read docs/progress/MASTER.md first and continue the first unfinished task. Keep its counts and the matching phase checklist current.

Preserve business subject/episode IDs, Emby identities, original movie_id user state, and manual mappings. Bulk catalog snapshots belong in an independent database. Never replace the active snapshot before validating the replacement. Keep archive, API enrichment, and user-owned state separate.

The user clarified on 2026-10-14 that this version is unreleased: do not build legacy anime database migrations or fallback catalogs. Stable identities still matter across routine snapshot refreshes. TMDB uses a configurable v3 API Key; the user-supplied default belongs in the Git-ignored local YAML, never in tracked files, logs, test output, or memory.

Images have distinct subject, character, person, and episode owners. Store metadata in SQLite and bytes in files. Episode stills require confirmed TMDB episode mappings; a poster is not an episode still.

Use official upstream data contracts. nz3u/bangumi-data is an AGPL reference, not a source to copy into this MIT project. Keep runtime archives, images, and credentials out of Git and release binaries.

Use temporary databases for verification. Run the existing frontend test:go-regression wrapper instead of invoking the destructive Redis test. Do not mutate the user's live database, deploy, commit, or push without specific authorization.

When all tasks pass, report evidence and remaining runtime limits. Ask what planning artifacts the user wants retained before any cleanup; never delete project documentation automatically.
