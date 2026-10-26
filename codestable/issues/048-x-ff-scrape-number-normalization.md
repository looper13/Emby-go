---
kind: issue
title: Normalize standard movie IDs before exact scrape matching
type: ff
status: closed
created: 2026-10-26
---

The scraper skipped AEGE-0015 when a provider returned AEGE-015 because comparison normalized separators but retained all numeric padding. Both now represent AEGE-015.

- Rule: NormalizeMovieID trims and uppercases standard PREFIX-NUMBER values, removes clearly separated leading vendor digits, strips numeric leading zeroes and pads to at least three digits. Unknown formats receive only trimming and uppercase conversion; numeric strings are not parsed into fixed-width integers.
- Integration: existing Normalize/SameNumber callers cover manual preview recommendations, exact-candidate ordering, batch/scheduled scraping and filename-derived expected IDs. NFO field generation also normalizes after known compact/series aliases gain a dash, keeping num/title/sorttitle consistent. Existing separator and MOSAIC compatibility remains; ambiguous compact forms retain old spelling equivalence without guessed boundaries. Different numbers and unknown suffixes stay distinct.
- Files: internal/metatube/number.go and tests; internal/scraper/apply.go, scraper_test.go and number_normalization_test.go; README.md. No mass metadata rewrite, file rename or change to duplicate-delete grouping. The user's TelevisionBrowser.vue and FavoritesPage.vue edits remain untouched, verified by identical before/after SHA-256 values.
- Validation: full metatube and scraper tests, server scrape/safety/cache regressions, go vet ./internal/metatube ./internal/scraper and go build ./... pass. New HTTP fixtures verify both AEGE padding directions, generic vendor digits, side-effect-free preview and actual normalized NFO writes. Selection tests exclude different numbers, numeric-prefix lookalikes and unknown version suffixes; pure helper tests cover all-zero values, long digit strings and idempotence.
- NFO tests: first full run has two Windows temp-file rename Access denied failures (TestSaveFileInfoOnlyDirectMovieElement/self_closing and TestMultiAudioFileInfoRoundTrip); both pass three consecutive focused reruns, and the subsequent full nfo run passes. These files are unchanged by this fix; no unconditional first-pass full-suite claim.
- Scope: current checkout only; no production scraper run or deployment. No associated project/epic spec exists; README documents the current matching rule and unchanged duplicate-delete boundary.
