import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { describe, expect, it } from 'vitest';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
describe('frozen visual assets', () => {
  for (const [legacy, migrated] of [
    ['style.css', 'src/styles/legacy.css'],
    ['favicon.ico', 'public/favicon.ico'],
  ]) {
    it(`keeps ${legacy} byte-for-byte`, () => {
      const original = readFileSync(path.join(root, 'internal/server/web', legacy));
      const copy = readFileSync(path.join(root, 'frontend', migrated));
      expect(copy.equals(original)).toBe(true);
    });
  }
  it('preserves the player version and license and keeps the original vendor baseline frozen', () => {
    const original = readFileSync(path.join(root, 'internal/server/web/vendor/artplayer.min.js'));
    const copy = readFileSync(path.join(root, 'frontend/src/vendor/artplayer.min.js'));
    expect(createHash('sha256').update(original).digest('hex')).toBe('4c9c9a565486c3e5f881fb0816518de2764a5fbad2c899fa97791c5b11fb28c8');
    const headerLength = original.indexOf('*/') + 2;
    expect(copy.subarray(0, headerLength).equals(original.subarray(0, headerLength))).toBe(true);
    expect(readFileSync(path.join(root, 'frontend/src/vendor/Artplayer.LICENSE'), 'utf8')).toContain('MIT License');
    // The documented local fullscreen cleanup patch is verified with real
    // metadata events and 20 player sessions in e2e/performance.spec.ts.
  });
});
