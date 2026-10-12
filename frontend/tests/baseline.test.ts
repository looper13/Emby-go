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
    // Git may check out this text asset with CRLF on Windows or LF on Linux.
    const normalizedOriginal = original.toString('utf8').replace(/\r\n/g, '\n');
    expect(createHash('sha256').update(normalizedOriginal).digest('hex')).toBe('f1db1fa3406aa6d41baa7c554749429a8e79f99d56a794bde23345eb9c07573f');
    const headerLength = original.indexOf('*/') + 2;
    expect(copy.subarray(0, headerLength).equals(original.subarray(0, headerLength))).toBe(true);
    expect(readFileSync(path.join(root, 'frontend/src/vendor/Artplayer.LICENSE'), 'utf8')).toContain('MIT License');
    // The documented local fullscreen cleanup patch is verified with real
    // metadata events and 20 player sessions in e2e/performance.spec.ts.
  });
});
