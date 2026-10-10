import fs from 'node:fs/promises';
import path from 'node:path';
import { repoRoot } from './environment.mjs';

// Mechanical redaction only: do not recollect, regenerate IDs, or alter response shapes.
const directory = path.join(repoRoot, 'vue-migration/fixtures/json');
const changed = [];
async function visit(dir) {
  for (const entry of await fs.readdir(dir, { withFileTypes: true })) {
    const file = path.join(dir, entry.name);
    if (entry.isDirectory() && entry.name !== 'live-runs') await visit(file);
    if (!entry.isFile() || !entry.name.endsWith('.json')) continue;
    const original = await fs.readFile(file, 'utf8');
    const walk = value => {
      if (typeof value === 'string') return value.replace(/[A-Z]:[\\/]Users[\\/][^\\/]+[\\/]AppData[\\/]Local[\\/]Temp[\\/]emby-go-live-vue01/g, 'TEMP_LEGACY');
      if (Array.isArray(value)) return value.map(walk);
      if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, /^(AccessToken|Pw|password)$/i.test(key) && typeof item === 'string' && item ? 'TOKEN' : walk(item)]));
      return value;
    };
    const before = JSON.parse(original);
    const after = walk(before);
    if (JSON.stringify(before) === JSON.stringify(after)) continue;
    const newline = original.includes('\r\n') ? '\r\n' : '\n';
    await fs.writeFile(file, (JSON.stringify(after, null, 2) + '\n').replaceAll('\n', newline));
    changed.push(path.relative(directory, file).replaceAll('\\', '/'));
  }
}
await visit(directory);
console.log(JSON.stringify({ changed }, null, 2));
