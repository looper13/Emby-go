---
kind: issue
title: Add client image HEAD and media compatibility endpoints
type: ff
status: open
created: 2026-10-26
---

Added image HEAD routes (including image indexes), authenticated MediaFolders with visible CollectionFolder DTOs, and authenticated SpecialFeatures returning an empty array for valid media/TV folder identities. Root, /emby and lowercase routes share the existing registration and access boundaries. Thumbnail HEAD responses include headers but no body; IsHidden query names are normalized and hidden-only folder queries return an empty list.

- Changes: server.go, media.go, compat_catalog.go, favorites.go, compat_media_test.go and README.md. No database migration, scan changes, full extras support or Logo/Banner/Art asset recognition; CONNECT remains unchanged.
- Validation: the three new HTTP contract tests pass, including original/resized HEAD, 304, missing images/indexes, probe exclusion, movie/episode/part/TV IDs, response shapes, empty folders, ordinary users and library/cross-user access. Store tests, go vet ./internal/server ./internal/store, go build ./... and git diff --check pass.
- Regression limits: the complete server suite fails TestMediaProbe (missing reframes/probeversion in NFO) and TestProbeCircuitBreaker (60-second timeout); both also fail in an untouched snapshot of HEAD 397270eaa1b625feeaa576075003f4c0d7be6937. A second server run excluding those tests has one TestLibraryPollingEndToEnd deletion conflict (409); this test then passes individually in both the modified checkout and original HEAD snapshot. These unrelated failures were not changed.
- codestable: no related spec/epic to update; README records the implemented contracts and limits. Implementation is locally complete; real-client confirmation is pending. Not committed, pushed or deployed.
