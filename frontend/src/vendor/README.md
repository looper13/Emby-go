# Preserved Player

`artplayer.min.js` is Artplayer 5.4.0 copied from the frozen legacy bundle, with one local lifecycle fix.

- Original SHA-256: `4c9c9a565486c3e5f881fb0816518de2764a5fbad2c899fa97791c5b11fb28c8`
- Patched SHA-256: `92e6d58c047cb681dcebd6f71137e95241348a9a519de00b472d3add3d7e389f`
- P7 fix: the fullscreen `change` and `error` callbacks are named and removed on the instance's `destroy` event. Upstream 5.4.0 registered two anonymous document listeners on `loadedmetadata` and left them behind after every player session. The real browser resource test opens/closes the player 20 times and compares listeners and timers with the warmed baseline. The legacy bundle remains untouched.
- Upstream: https://github.com/zhw2590582/ArtPlayer
- The original copyright and MIT license notice remain in the file.
- `Artplayer.LICENSE` contains the upstream MIT terms from `master/LICENSE`, retrieved 2026-10-10; the bundled header retains its original 2017-2026 attribution.
- P4 loads this preserved file only when a PlayerDialog opens. Vite publishes it as a separate hashed dynamic chunk; the loader uses its default export (with a global fallback for the original UMD). The component owns and destroys its instance. Never also load the legacy script tag.
