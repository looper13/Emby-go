---
kind: issue
title: Use episode thumbnails for missing Primary covers
type: ff
status: closed
created: 2026-10-26
---

Episodes with only same-name -thumb artwork exposed Thumb but no Primary, while the television browser required Primary and rendered text placeholders without requesting an image.

- Change: the shared request-time artwork resolver uses episode landscape artwork as Primary only when no poster resolves, retaining Thumb and existing poster priority. Movie, series and season rules are unchanged; no file rename, scan or database migration is needed.
- Files: internal/server/artwork.go, internal/server/episode_artwork_test.go, frontend/src/components/media/TelevisionBrowser.vue, frontend/tests/television.test.ts and README.md. The browser's Thumb fallback applies only to Episode; image visibility remains respected. Pre-existing client HEAD/MediaFolders/SpecialFeatures work is preserved.
- Validation: episode HTTP regression covers real mixed-library scanning, distinct same-directory episode thumbs, missing art, poster priority, image tags/inventory, GET and resized HEAD, root/emby and lowercase routes, and unchanged movie/series/season Primary behavior. Television/account/artwork server regressions, imageutil tests, go vet ./internal/server and embedded Go build pass. Frontend targeted tests pass 21/21; typecheck and production build pass.
- Browser: isolated Go/Redis/Vite fixture in Chrome; 12 thumb-only episodes load actual 320x180 covers on desktop and at 390px, without horizontal overflow. Episode detail return retains the series/season. Removing Primary only from the intercepted episode-list response verifies all 12 Thumb URLs still decode. Desktop/mobile screenshots are retained under ignored output/playwright/.
- Scope: local fixture verification is not NAS, Linux or real Emby-client acceptance. No commit, push or deployment. No associated project/epic spec exists; README is synchronized.
