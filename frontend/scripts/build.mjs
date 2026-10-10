import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { build } from 'vite';
import { buildPaths, inspectDistribution, publishDistribution, withBuildLock } from './build-lib.mjs';
import './check-toolchain.mjs';

const paths = buildPaths(fileURLToPath(new URL('../../', import.meta.url)));
await withBuildLock(paths, async () => {
  await build({ configFile: fileURLToPath(new URL('../vite.config.ts', import.meta.url)) });
  let commit = 'unknown';
  let dirty = true;
  try {
    commit = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: paths.root, encoding: 'utf8', windowsHide: true }).trim();
    dirty = execFileSync('git', ['status', '--porcelain', '--', 'frontend', 'internal/server'], {
      cwd: paths.root, encoding: 'utf8', windowsHide: true,
    }).trim() !== '';
  } catch {
    // A source archive has no .git; this is explicitly recorded in its build manifest.
  }
  const info = await inspectDistribution(paths.dist, { commit, dirty });
  await publishDistribution(paths, info);
  console.log(`Published ${info.files.length} verified UI files to ${paths.target}`);
});
